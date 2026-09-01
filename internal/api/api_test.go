package api_test

import (
	"net/http/httptest"
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
