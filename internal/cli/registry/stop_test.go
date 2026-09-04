package registry_test

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/registry"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func TestStopWaitsForBroker(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(api.New(st, nil, api.WithShutdown(func() { srv.Close() })).Handler())

	if err := registry.Stop(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if registry.IsUp(context.Background(), srv.URL) {
		t.Fatal("broker is still running after Stop returned")
	}
}

func TestServeLogsLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	err := registry.Serve(ctx, &cmdutil.Options{DB: filepath.Join(root, "sessions.db")}, registry.Config{
		Addr: "127.0.0.1:0", Log: io.Discard, OnReady: func(string) { cancel() },
	})
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(root, "broker.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"broker started", "broker shutting down", "broker stopped", "version="} {
		if !strings.Contains(string(log), want) {
			t.Errorf("broker.log missing %q: %s", want, log)
		}
	}
}
