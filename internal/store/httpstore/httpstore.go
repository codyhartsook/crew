// Package httpstore implements store.Store against the registry HTTP API. It
// runs the local store's conformance suite, so callers cannot tell them apart.
package httpstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

// defaultTimeout bounds a hook's call to the server. A hook runs in front of
// the user, so an unreachable server must fail quickly.
const defaultTimeout = 5 * time.Second

type Store struct {
	baseURL string
	client  *http.Client
}

var _ store.Store = (*Store)(nil)

type Option func(*Store)

func WithHTTPClient(c *http.Client) Option {
	return func(s *Store) { s.client = c }
}

func New(baseURL string, opts ...Option) *Store {
	s := &Store{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// No resources to release.
func (s *Store) Close() error { return nil }

func (s *Store) Upsert(ctx context.Context, sess *session.Session) error {
	_, err := s.do(ctx, http.MethodPost, "/v1/sessions", sess, sess)
	return err
}

func (s *Store) End(ctx context.Context, key string, at time.Time, reason string) error {
	body := api.EndRequest{EndedAt: &at, Reason: reason}
	_, err := s.do(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(key)+"/end", body, nil)
	return err
}

func (s *Store) Touch(ctx context.Context, key string, at time.Time) error {
	body := api.TouchRequest{At: &at}
	_, err := s.do(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(key)+"/touch", body, nil)
	return err
}

func (s *Store) SetUsage(ctx context.Context, key string, u *usage.Snapshot) error {
	_, err := s.do(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(key)+"/usage", u, nil)
	return err
}

func (s *Store) Get(ctx context.Context, key string) (*session.Session, error) {
	var sess session.Session
	if _, err := s.do(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(key), nil, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) List(ctx context.Context, f store.Filter) ([]*session.Session, error) {
	q := url.Values{}
	if f.Harness != "" {
		q.Set("harness", string(f.Harness))
	}
	if f.Status != "" {
		q.Set("status", string(f.Status))
	}
	if f.RepoName != "" {
		q.Set("repo", f.RepoName)
	}
	if f.RepoRoot != "" {
		q.Set("repo_root", f.RepoRoot)
	}
	if f.PooledOnly {
		q.Set("pooled", "true")
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}

	path := "/v1/sessions"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp api.ListResponse
	if _, err := s.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Sessions == nil {
		resp.Sessions = []*session.Session{}
	}
	return resp.Sessions, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.do(ctx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(key), nil, nil)
	return err
}

// do issues one request, decoding a 2xx body into out when out is non-nil and
// mapping a 404 onto store.ErrNotFound.
func (s *Store) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return resp.StatusCode, statusError(method, path, resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return resp.StatusCode, nil
}

func statusError(method, path string, resp *http.Response) error {
	var body api.ErrorResponse
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body)
	detail := body.Error
	if detail == "" {
		detail = resp.Status
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", detail, store.ErrNotFound)
	}
	return fmt.Errorf("%s %s: %s", method, path, detail)
}
