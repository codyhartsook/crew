// Package api exposes the session store over HTTP. The surface is a direct
// projection of store.Store, so a remote client can present itself as just
// another implementation.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/codyhartsook/multiplayer/internal/documents"
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
	store     backend
	log       *slog.Logger
	ui        http.Handler
	table     http.Handler
	openRoom  func(string) error
	documents func(string) (string, error)
}

type Option func(*Server)

// WithUI mounts a web interface at the root path. Without it the server is
// API-only, which is what the tests and any headless use want.
func WithUI(h http.Handler) Option {
	return func(s *Server) { s.ui = h }
}

// WithTableUI mounts the row-per-session view at /table, beside the dashboard.
func WithTableUI(h http.Handler) Option {
	return func(s *Server) { s.table = h }
}

// WithRoomOpener lets the local dashboard reveal a directory on this machine.
// It is given the path to open, not a room key.
func WithRoomOpener(open func(path string) error) Option {
	return func(s *Server) { s.openRoom = open }
}

// WithDocuments mounts room document stores, resolved per room key. Without
// it the server serves no documents.
func WithDocuments(dir func(string) (string, error)) Option {
	return func(s *Server) { s.documents = dir }
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
	mux.HandleFunc("GET /v1/meta", s.meta)
	if s.openRoom != nil && s.documents != nil {
		mux.HandleFunc("POST /v1/rooms/open", s.local(s.openRoomDocuments))
	}
	if s.documents != nil {
		mux.HandleFunc("GET /v1/rooms/documents", s.local(s.listDocuments))
		mux.HandleFunc("POST /v1/rooms/documents", s.local(s.addDocument))
		mux.HandleFunc("DELETE /v1/rooms/documents", s.local(s.removeDocument))
		mux.HandleFunc("DELETE /v1/entries/{id}", s.local(s.deleteEntry))
	}
	if s.table != nil {
		mux.Handle("GET /table", s.table)
	}
	if s.ui != nil {
		// "/{$}" matches the root path exactly, so the interface cannot shadow
		// an API route or swallow unknown paths.
		mux.Handle("GET /{$}", s.ui)
	}
	return s.logRequests(mux)
}

// local limits operator endpoints to the dashboard on this machine.
func (s *Server) local(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		origin := r.Header.Get("Origin")
		if err != nil || !net.ParseIP(host).IsLoopback() ||
			(origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host) {
			writeError(w, http.StatusForbidden, errors.New("local dashboard access only"))
			return
		}
		next(w, r)
	}
}

// errBadRequest marks a client mistake, so one fail path can tell it from a
// store failure.
type errBadRequest struct{ error }

// roomKey reads and verifies the room a request names. A room exists once
// anyone has joined it or posted in it.
func (s *Server) roomKey(r *http.Request) (string, error) {
	key := r.URL.Query().Get("room")
	if key == "" {
		return "", errBadRequest{errors.New("room is required")}
	}
	members, err := s.store.Members(r.Context(), key)
	if err != nil {
		return "", err
	}
	if len(members) > 0 {
		return key, nil
	}
	entries, err := s.store.Entries(r.Context(), room.Filter{Rooms: []string{key}, Limit: 1})
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", store.ErrNotFound
	}
	return key, nil
}

// roomDir resolves a verified room's document directory.
func (s *Server) roomDir(r *http.Request) (string, string, error) {
	key, err := s.roomKey(r)
	if err != nil {
		return "", "", err
	}
	dir, err := s.documents(key)
	return key, dir, err
}

// openRoomDocuments reveals a room's folder: the generated transcript and the
// documents beside it. The transcript is regenerated on the way out, so what
// opens is the room as it is now rather than as it was last time.
func (s *Server) openRoomDocuments(w http.ResponseWriter, r *http.Request) {
	key, dir, err := s.roomDir(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	target := filepath.Dir(dir)
	here := room.Room{Key: key, Scope: s.scopeOf(r, key), Name: room.NameFor(key)}
	if _, err := documents.WriteTranscript(r.Context(), s.store, s.store, dir, here); err != nil {
		// The documents are still worth opening without their transcript.
		s.log.Error("write room transcript", "room", key, "error", err)
	}
	if err := s.openRoom(target); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DocumentsResponse wraps a room's document listing.
type DocumentsResponse struct {
	Documents []documents.Document `json:"documents"`
	Count     int                  `json:"count"`
}

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request) {
	_, dir, err := s.roomDir(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	docs, err := documents.List(dir)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, DocumentsResponse{Documents: docs, Count: len(docs)})
}

// maxDocument caps an upload: documents are plans and reports, not build output.
const maxDocument = 25 << 20

func (s *Server) addDocument(w http.ResponseWriter, r *http.Request) {
	key, dir, err := s.roomDir(r)
	if err != nil {
		s.fail(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDocument+1<<16)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read uploaded document: %w", err))
		return
	}
	defer file.Close()
	if header.Size > maxDocument {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Errorf("document is %d bytes; the limit is %d", header.Size, maxDocument))
		return
	}

	// Filename is already its own base: RFC 7578 forbids using the directory
	// part and mime/multipart strips it. Add still validates what is left.
	name, err := documents.Add(dir, header.Filename, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.announce(r, key, documents.PublishedNote+name)
	writeJSON(w, http.StatusOK, documents.Document{Name: name, Size: header.Size, ModTime: time.Now().UTC()})
}

func (s *Server) removeDocument(w http.ResponseWriter, r *http.Request) {
	key, dir, err := s.roomDir(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	if _, err := documents.Remove(dir, name); err != nil {
		if errors.Is(err, documents.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.announce(r, key, documents.RemovedNote+name)
	w.WriteHeader(http.StatusNoContent)
}

// deleteEntry removes a post and its replies, whoever wrote them.
func (s *Server) deleteEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("id must be a positive integer"))
		return
	}
	removed, err := s.store.DeleteThread(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, fmt.Errorf("entry %d not found", id))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// announce records a document change in the room timeline. The file has
// already moved, so a failed announcement is logged, not returned.
func (s *Server) announce(r *http.Request, key, body string) {
	e := &room.Entry{
		Room: key, Scope: s.scopeOf(r, key), Mode: room.ModeNote,
		Author: room.HumanAuthor(), Body: body, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.Post(r.Context(), e); err != nil {
		s.log.Error("announce document change", "room", key, "error", err)
	}
}

// scopeOf recovers a room's scope from whoever is in it or what is posted there.
func (s *Server) scopeOf(r *http.Request, key string) room.Scope {
	if members, err := s.store.Members(r.Context(), key); err == nil && len(members) > 0 {
		return members[0].Scope
	}
	if entries, err := s.store.Entries(r.Context(), room.Filter{Rooms: []string{key}, Limit: 1}); err == nil && len(entries) > 0 {
		return entries[0].Scope
	}
	return room.ScopeFolder
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

// MetaResponse describes the machine whose sessions this registry holds. The
// store records absolute paths; a client needs Home to show them the way the
// engineer reads them.
type MetaResponse struct {
	Home string `json:"home"`
	Host string `json:"host,omitempty"`
}

// ErrorResponse is the body of every non-2xx reply.
type ErrorResponse struct {
	Error string `json:"error"`
}

// meta reports the registry's own machine. A failure to resolve either field is
// not worth an error: the client falls back to showing absolute paths.
func (s *Server) meta(w http.ResponseWriter, _ *http.Request) {
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, MetaResponse{Home: home, Host: host})
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
	for _, value := range q["mode"] {
		mode := room.Mode(value)
		if !mode.Valid() {
			return room.Filter{}, errors.New("unknown mode " + value)
		}
		f.Modes = append(f.Modes, mode)
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
	var bad errBadRequest
	if errors.As(err, &bad) {
		writeError(w, http.StatusBadRequest, bad.error)
		return
	}
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
