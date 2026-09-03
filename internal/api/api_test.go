package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/httpstore"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
	"github.com/codyhartsook/multiplayer/internal/store/storetest"
)

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
