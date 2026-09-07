package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/documents"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/httpstore"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
	"github.com/codyhartsook/multiplayer/internal/store/storetest"
)

// Opening a room reveals its whole folder: the generated transcript of what
// happened there, and the documents beside it. Opening only the documents is
// the bug this guards.
func TestOpenRoomDocuments(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := t.Context()
	if err := st.Upsert(ctx, &session.Session{
		ID: "one", Harness: session.HarnessCodex,
		Status: session.StatusActive, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// The store names sessions itself, so the transcript is checked against
	// whatever alias it handed out.
	sess, err := st.Get(ctx, "codex:one")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Join(ctx, &room.Membership{
		SessionKey: "codex:one", Room: "/repo", Scope: room.ScopeWorktree, JoinedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Post(ctx, &room.Entry{
		Room: "/repo", Scope: room.ScopeWorktree, Mode: room.ModeNote, Author: "codex:one",
		Body: "the broker now logs its lifecycle", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	dir, err := documents.Dir(root, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := documents.Add(dir, "plan.md", strings.NewReader("the plan")); err != nil {
		t.Fatal(err)
	}

	opened := ""
	handler := api.New(st, nil,
		api.WithDocuments(func(key string) (string, error) { return documents.Dir(root, key) }),
		api.WithRoomOpener(func(path string) error {
			opened = path
			return nil
		}),
	).Handler()

	rec := call(t, handler, http.MethodPost, "http://localhost/v1/rooms/open?room=%2Frepo")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("open room = %d, want 204: %s", rec.Code, rec.Body)
	}

	// The folder that opens is the room's own, not the documents inside it.
	if want := filepath.Dir(dir); opened != want {
		t.Errorf("opened %q, want the room folder %q", opened, want)
	}

	transcript, err := os.ReadFile(filepath.Join(filepath.Dir(dir), documents.SnapshotName))
	if err != nil {
		t.Fatalf("no transcript in the room folder: %v", err)
	}
	for _, want := range []string{
		"the broker now logs its lifecycle", // the activity
		sess.Alias,                          // who did it
		"plan.md",                           // and the documents
		"## Timeline",
	} {
		if !strings.Contains(string(transcript), want) {
			t.Errorf("transcript is missing %q\n%s", want, transcript)
		}
	}
}

// A stale transcript is worse than none: it is regenerated every time.
func TestOpenRoomRegeneratesTranscript(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	if err := st.Join(ctx, &room.Membership{
		SessionKey: "codex:one", Room: "/repo", Scope: room.ScopeWorktree, JoinedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	handler := api.New(st, nil,
		api.WithDocuments(func(key string) (string, error) { return documents.Dir(root, key) }),
		api.WithRoomOpener(func(string) error { return nil }),
	).Handler()
	open := func() string {
		t.Helper()
		if rec := call(t, handler, http.MethodPost, "http://localhost/v1/rooms/open?room=%2Frepo"); rec.Code != http.StatusNoContent {
			t.Fatalf("open room = %d: %s", rec.Code, rec.Body)
		}
		dir, err := documents.Dir(root, "/repo")
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(filepath.Dir(dir), documents.SnapshotName))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	if got := open(); strings.Contains(got, "posted after the first open") {
		t.Fatal("transcript already carries an entry nobody posted")
	}
	if err := st.Post(ctx, &room.Entry{
		Room: "/repo", Scope: room.ScopeWorktree, Mode: room.ModeNote, Author: "codex:one",
		Body: "posted after the first open", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if got := open(); !strings.Contains(got, "posted after the first open") {
		t.Errorf("transcript was not regenerated:\n%s", got)
	}
}

// Cross-origin and off-host callers are refused, and an unknown room 404s.
func TestOpenRoomIsLocalOnly(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Join(t.Context(), &room.Membership{
		SessionKey: "codex:one", Room: "/repo", Scope: room.ScopeWorktree, JoinedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	opened := ""
	handler := api.New(st, nil,
		api.WithDocuments(func(key string) (string, error) { return documents.Dir(root, key) }),
		api.WithRoomOpener(func(path string) error { opened = path; return nil }),
	).Handler()

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/rooms/open?room=%2Frepo", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || opened != "" {
		t.Errorf("cross-origin open = (%d, %q), want (403, empty)", rec.Code, opened)
	}

	if rec := call(t, handler, http.MethodPost, "http://localhost/v1/rooms/open?room=%2Fmissing"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room = %d, want 404", rec.Code)
	}
}

// TestAPIConformance runs the store conformance suite through the HTTP API, so
// the server and its client are held to exactly the semantics the local store
// provides.
func TestAPIConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		backing, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { backing.Close() })

		srv := httptest.NewServer(api.New(backing, nil).Handler())
		t.Cleanup(srv.Close)

		return httpstore.New(srv.URL, httpstore.WithHTTPClient(srv.Client()))
	})
}

// The dashboard renders paths relative to home, which only the registry's own
// machine can report.
func TestMetaReportsHome(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv := httptest.NewServer(api.New(st, nil).Handler())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/v1/meta")
	if err != nil {
		t.Fatalf("GET /v1/meta: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var meta api.MetaResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	if meta.Home != home {
		t.Errorf("home = %q, want %q", meta.Home, home)
	}
}
