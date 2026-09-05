package detect_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/detect"
)

// The whole point of the marker: a session started deep inside the folder
// resolves to the same anchor as one started at the top.
func TestDetectFolderAnchorFromSubdirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "notes")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	anchorDir(t, root, "")

	fromRoot := mustDetect(t, root)
	fromSub := mustDetect(t, sub)

	if fromSub.Folder == nil {
		t.Fatal("Folder = nil, want the anchor")
	}
	if fromSub.Folder.Root != fromRoot.Folder.Root {
		t.Errorf("Root = %q from the subdirectory, want %q", fromSub.Folder.Root, fromRoot.Folder.Root)
	}
	if fromSub.Folder.Name != "notes" {
		t.Errorf("Name = %q, want the directory name", fromSub.Folder.Name)
	}
	if fromSub.CWD == fromSub.Folder.Root {
		t.Error("CWD should still record where the session actually started")
	}
}

func TestDetectFolderAnchorNameFromMarker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	anchorDir(t, root, "field-notes")

	loc := mustDetect(t, root)
	if loc.Folder == nil {
		t.Fatal("Folder = nil, want the anchor")
	}
	if loc.Folder.Name != "field-notes" {
		t.Errorf("Name = %q, want the name recorded in the marker", loc.Folder.Name)
	}
}

// Honouring a marker inside a checkout would split that repository's room.
func TestDetectFolderAnchorIgnoredInsideCheckout(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "widget"))
	anchorDir(t, repo, "")

	loc := mustDetect(t, repo)
	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the checkout to win")
	}
	if loc.Folder != nil {
		t.Errorf("Folder = %+v, want nil inside a checkout", loc.Folder)
	}
}

// Unanchored is ordinary, not an error: the working directory stands alone.
func TestDetectUnanchoredFolder(t *testing.T) {
	loc := mustDetect(t, t.TempDir())
	if loc.Folder != nil {
		t.Errorf("Folder = %+v, want nil without a marker", loc.Folder)
	}
	if loc.CWD == "" {
		t.Error("CWD should be recorded anyway")
	}
}

// No git binary is a definite answer everywhere on this machine, so the folder
// anchor still gets its turn rather than the chain giving up.
func TestDetectFolderAnchorWithoutGit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	anchorDir(t, root, "")
	t.Setenv("PATH", t.TempDir())

	loc, err := detect.New().Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect without git: %v", err)
	}
	if loc.Folder == nil {
		t.Fatal("Folder = nil, want the anchor found without git on PATH")
	}
}

// The walk stops at the home directory. A marker above it would pull every
// folder on the machine into one room.
func TestDetectFolderAnchorStopsAtHome(t *testing.T) {
	above := t.TempDir()
	home := filepath.Join(above, "home")
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	anchorDir(t, above, "")
	t.Setenv("HOME", home)

	if loc := mustDetect(t, work); loc.Folder != nil {
		t.Errorf("Folder = %+v, want nil for a marker above home", loc.Folder)
	}

	// Home itself is still checked, so anchoring it deliberately works.
	anchorDir(t, home, "")
	loc := mustDetect(t, work)
	if loc.Folder == nil {
		t.Fatal("Folder = nil, want a marker at home to be honoured")
	}
	if loc.Folder.Root != norm(t, home) {
		t.Errorf("Root = %q, want %q", loc.Folder.Root, norm(t, home))
	}
}

func anchorDir(t *testing.T, root, name string) {
	t.Helper()
	marker := filepath.Join(root, detect.FolderMarker)
	if err := os.MkdirAll(marker, 0o755); err != nil {
		t.Fatalf("anchor %s: %v", root, err)
	}
	if name == "" {
		return
	}
	body := []byte(`{"name": "` + name + `"}`)
	if err := os.WriteFile(filepath.Join(marker, detect.FolderConfig), body, 0o644); err != nil {
		t.Fatalf("write marker config: %v", err)
	}
}
