package ui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
	"github.com/codyhartsook/multiplayer/internal/ui"
)

func TestHandlerServesPage(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}

	body := rec.Body.String()
	// The page must carry its own styles and script: nothing is fetched from a
	// CDN, so the dashboard works with no network at all.
	for _, want := range []string{"<title>multiplayer</title>", "<style>", "/v1/sessions"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	for _, unwanted := range []string{"src=\"http", "href=\"http"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("page loads an external resource (%q); it must be self-contained", unwanted)
		}
	}
}

// The interface is mounted at the root only, so it can never shadow an API
// route or absorb requests for paths that should 404.
func TestUIDoesNotShadowAPI(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv := httptest.NewServer(api.New(st, nil, api.WithUI(ui.Handler())).Handler())
	t.Cleanup(srv.Close)

	cases := []struct {
		path string
		want int
		html bool
	}{
		{"/", http.StatusOK, true},
		{"/v1/sessions", http.StatusOK, false},
		{"/healthz", http.StatusOK, false},
		{"/nonsense", http.StatusNotFound, false},
	}
	for _, tc := range cases {
		resp, err := srv.Client().Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != tc.want {
			t.Errorf("GET %s: status = %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
		if isHTML := strings.Contains(string(body), "<title>"); isHTML != tc.html {
			t.Errorf("GET %s: served HTML = %v, want %v", tc.path, isHTML, tc.html)
		}
	}
}

func TestAPIOnlyServerHasNoUI(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv := httptest.NewServer(api.New(st, nil).Handler())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when no UI is mounted", resp.StatusCode)
	}
}
