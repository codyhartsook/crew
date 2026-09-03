package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A "go run" binary lives in the build cache and is deleted on exit; recording
// its path would leave hooks that fail silently forever.
func TestTransient(t *testing.T) {
	for _, path := range []string{
		"/var/folders/xx/T/go-build123/b001/exe/crew",
		filepath.Join(os.TempDir(), "mp"),
	} {
		if !transient(path) {
			t.Errorf("transient(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"/usr/local/bin/crew", "/Users/x/.local/bin/crew"} {
		if transient(path) {
			t.Errorf("transient(%q) = true, want false", path)
		}
	}
}

func TestInitViewStaysPlainOutsideATerminal(t *testing.T) {
	var out strings.Builder
	view := newInitView(&out)
	if got := view.heading("Setup"); got != "Setup" {
		t.Errorf("heading = %q, want plain text", got)
	}
}
