package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/session"
)

func TestNotifyUsesControlRelay(t *testing.T) {
	pid := os.Getpid()
	controlPath := filepath.Join(controlSocketDir, fmt.Sprintf("%d.sock", pid))
	if err := os.MkdirAll(filepath.Dir(controlPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controlPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(controlPath) })

	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "relayed")
	script := filepath.Join(binDir, "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \"$CREW_RELAY_MARKER\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CREW_RELAY_MARKER", marker)

	s := &session.Session{ID: "test", PID: pid}
	if err := Notify(context.Background(), s, "check inbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("control relay was not used")
	}
}
