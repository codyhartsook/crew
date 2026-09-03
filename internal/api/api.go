// Package api exposes the session store over HTTP. The surface is a direct
// projection of store.Store, so a remote client can present itself as just
// another implementation.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

// backend is the local database the dashboard serves. A remote registry is a
// session client, not a server backend: rooms always live beside the worktree.
type backend interface {
	store.Store
	store.RoomStore
}

type Server struct {
	store backend
	log   *slog.Logger
	ui    http.Handler
}

type Option func(*Server)

// WithUI mounts a web interface at the root path. Without it the server is
// API-only, which is what the tests and any headless use want.
func WithUI(h http.Handler) Option {
	return func(s *Server) { s.ui = h }
}

// New returns a Server backed by st. A nil logger discards request logs.
func New(st backend, log *slog.Logger, opts ...Option) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Server{store: st, log: log}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/sessions", s.listSessions)
	mux.HandleFunc("POST /v1/sessions", s.upsertSession)
	mux.HandleFunc("GET /v1/sessions/{key}", s.getSession)
	mux.HandleFunc("DELETE /v1/sessions/{key}", s.deleteSession)
	mux.HandleFunc("POST /v1/sessions/{key}/end", s.endSession)
	mux.HandleFunc("POST /v1/sessions/{key}/touch", s.touchSession)
	mux.HandleFunc("POST /v1/sessions/{key}/usage", s.setSessionUsage)
	mux.HandleFunc("GET /v1/entries", s.listEntries)
	mux.HandleFunc("GET /v1/state", s.listState)
	if s.ui != nil {
		// "/{$}" matches the root path exactly, so the interface cannot shadow
		// an API route or swallow unknown paths.
		mux.Handle("GET /{$}", s.ui)
	}
	return s.logRequests(mux)
}

// EndRequest is the body of POST /v1/sessions/{key}/end. An omitted EndedAt
// means "now".
type EndRequest struct {
	EndedAt *time.Time `json:"ended_at,omitempty"`
	Reason  string     `json:"reason,omitempty"`
}

// TouchRequest is the body of POST /v1/sessions/{key}/touch. An omitted At
// means "now".
type TouchRequest struct {
	At *time.Time `json:"at,omitempty"`
}

// ListResponse wraps a session listing so the payload can grow without breaking
// clients that already parse it.
type ListResponse struct {
	Sessions []*session.Session `json:"sessions"`
	Count    int                `json:"count"`
}

// EntriesResponse wraps a room entry listing.
type EntriesResponse struct {
	Entries []*room.Entry `json:"entries"`
	Count   int           `json:"count"`
}

// StateResponse wraps a room state listing.
type StateResponse struct {
	State []*room.State `json:"state"`
	Count int           `json:"count"`
}

// ErrorResponse is the body of every non-2xx reply.
type ErrorResponse struct {
	Error string `json:"error"`
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	filter, err := filterFromQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sessions, err := s.store.List(r.Context(), filter)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ListResponse{Sessions: sessions, Count: len(sessions)})
}

func (s *Server) listEntries(w http.ResponseWriter, r *http.Request) {
	f, err := entryFilterFromQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	entries, err := s.store.Entries(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, EntriesResponse{Entries: entries, Count: len(entries)})
}

func entryFilterFromQuery(r *http.Request) (room.Filter, error) {
	q := r.URL.Query()
	f := room.Filter{Rooms: q["room"]}
	for _, k := range q["kind"] {
		kind := room.Kind(k)
		if !kind.Valid() {
			return room.Filter{}, errors.New("unknown kind " + k)
		}
		f.Kinds = append(f.Kinds, kind)
	}
	if v := q.Get("open"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return room.Filter{}, errors.New("open must be a boolean")
		}
		f.OpenOnly = b
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return room.Filter{}, errors.New("limit must be a non-negative integer")
		}
		f.Limit = n
	}
	return f, nil
}

func (s *Server) listState(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := room.StateFilter{Rooms: q["room"], Prefix: q.Get("prefix")}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, errors.New("limit must be a non-negative integer"))
			return
		}
		f.Limit = n
	}
	values, err := s.store.States(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, StateResponse{State: values, Count: len(values)})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Get(r.Context(), r.PathValue("key"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) upsertSession(w http.ResponseWriter, r *http.Request) {
	var sess session.Session
	if err := decodeJSON(r, &sess); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if sess.ID == "" {
		writeError(w, http.StatusBadRequest, errors.New("session id is required"))
		return
	}
	if !sess.Harness.Valid() {
		writeError(w, http.StatusBadRequest, errors.New("unknown harness "+string(sess.Harness)))
		return
	}
	if err := s.store.Upsert(r.Context(), &sess); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &sess)
}

func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	var req EndRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	at := time.Now().UTC()
	if req.EndedAt != nil {
		at = req.EndedAt.UTC()
	}
	if err := s.store.End(r.Context(), r.PathValue("key"), at, req.Reason); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) touchSession(w http.ResponseWriter, r *http.Request) {
	var req TouchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	at := time.Now().UTC()
	if req.At != nil {
		at = req.At.UTC()
	}
	if err := s.store.Touch(r.Context(), r.PathValue("key"), at); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setSessionUsage(w http.ResponseWriter, r *http.Request) {
	var u usage.Snapshot
	if err := decodeJSON(r, &u); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetUsage(r.Context(), r.PathValue("key"), &u); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.Context(), r.PathValue("key")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail maps a store error onto a status code.
func (s *Server) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	s.log.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, err)
}

func filterFromQuery(r *http.Request) (store.Filter, error) {
	q := r.URL.Query()
	f := store.Filter{
		Harness:  session.Harness(q.Get("harness")),
		Status:   session.Status(q.Get("status")),
		RepoName: q.Get("repo"),
		RepoRoot: q.Get("repo_root"),
	}
	// "treehouse" is the old spelling, kept so an older dashboard or client
	// keeps working against a new server.
	v := q.Get("pooled")
	if v == "" {
		v = q.Get("treehouse")
	}
	if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return store.Filter{}, errors.New("pooled must be a boolean")
		}
		f.PooledOnly = b
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return store.Filter{}, errors.New("limit must be a non-negative integer")
		}
		f.Limit = n
	}
	if f.Harness != "" && !f.Harness.Valid() {
		return store.Filter{}, errors.New("unknown harness " + string(f.Harness))
	}
	return f, nil
}

// decodeJSON reads a JSON body, tolerating an empty one so that requests whose
// fields are all optional need not send a body at all.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, ErrorResponse{Error: err.Error()})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}
