package registry_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/registry"
)

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
