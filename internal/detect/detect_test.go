package detect_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/detect"
)

func TestDetectOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	loc := mustDetect(t, dir)

	if loc.Repo != nil {
		t.Errorf("Repo = %+v, want nil outside a checkout", loc.Repo)
	}
	if loc.Treehouse != nil {
		t.Errorf("Treehouse = %+v, want nil", loc.Treehouse)
	}
	if loc.CWD == "" {
		t.Error("CWD should be recorded even outside a checkout")
	}
}

func TestDetectPlainCheckout(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "widget"))
	loc := mustDetect(t, repo)

	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the checkout")
	}
	if loc.Repo.Name != "widget" {
		t.Errorf("Name = %q, want %q", loc.Repo.Name, "widget")
	}
	if loc.Repo.Root != loc.Repo.MainRoot {
		t.Errorf("Root %q and MainRoot %q should match in a primary checkout", loc.Repo.Root, loc.Repo.MainRoot)
	}
	if loc.Repo.IsWorktree {
		t.Error("IsWorktree = true, want false for a primary checkout")
	}
	if loc.Repo.Branch == "" {
		t.Error("Branch is empty, want the checked-out branch")
	}
	if loc.Repo.Detached {
		t.Error("Detached = true, want false")
	}
	if loc.Repo.Head == "" {
		t.Error("Head is empty, want the commit sha")
	}
	if loc.Treehouse != nil {
		t.Errorf("Treehouse = %+v, want nil for an ordinary checkout", loc.Treehouse)
	}
}

// A hook can fire from anywhere inside the checkout, so a subdirectory must
// resolve to the same repository root as the top of the tree.
func TestDetectFromSubdirectory(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "widget"))
	sub := filepath.Join(repo, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fromRoot := mustDetect(t, repo)
	fromSub := mustDetect(t, sub)

	if fromSub.Repo == nil {
		t.Fatal("Repo = nil in a subdirectory of a checkout")
	}
	if fromSub.Repo.Root != fromRoot.Repo.Root {
		t.Errorf("Root = %q from subdirectory, want %q", fromSub.Repo.Root, fromRoot.Repo.Root)
	}
	if fromSub.Repo.MainRoot != fromRoot.Repo.MainRoot {
		t.Errorf("MainRoot = %q from subdirectory, want %q", fromSub.Repo.MainRoot, fromRoot.Repo.MainRoot)
	}
}

func TestDetectLinkedWorktree(t *testing.T) {
	base := t.TempDir()
	main := initRepo(t, filepath.Join(base, "widget"))
	linked := filepath.Join(base, "elsewhere", "widget")
	addWorktree(t, main, linked)

	loc := mustDetect(t, linked)
	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the linked worktree")
	}
	if !loc.Repo.IsWorktree {
		t.Error("IsWorktree = false, want true for a linked worktree")
	}
	if loc.Repo.MainRoot != norm(t, main) {
		t.Errorf("MainRoot = %q, want %q", loc.Repo.MainRoot, norm(t, main))
	}
	if loc.Repo.Root != norm(t, linked) {
		t.Errorf("Root = %q, want %q", loc.Repo.Root, norm(t, linked))
	}
	// The name comes from the repository, not from wherever the worktree sits.
	if loc.Repo.Name != "widget" {
		t.Errorf("Name = %q, want %q", loc.Repo.Name, "widget")
	}
	if loc.Treehouse != nil {
		t.Errorf("Treehouse = %+v, want nil for a worktree outside a pool", loc.Treehouse)
	}
}

// A treehouse pool is a manifest directory holding numbered slots, each of
// which contains a linked worktree of the repository.
func TestDetectTreehouseWorktree(t *testing.T) {
	base := t.TempDir()
	main := initRepo(t, filepath.Join(base, "widget"))

	pool := filepath.Join(base, "pool", "widget-abc123")
	slotWorktree := filepath.Join(pool, "3", "widget")
	addWorktree(t, main, slotWorktree)
	writeManifest(t, pool, map[string]any{
		"worktrees": []any{
			map[string]any{"name": "1", "path": filepath.Join(pool, "1", "widget")},
			map[string]any{
				"name":         "3",
				"path":         slotWorktree,
				"leased":       true,
				"lease_id":     "70b6d0fd",
				"lease_holder": "agent:build-the-thing",
			},
		},
	})

	loc := mustDetect(t, slotWorktree)
	if loc.Treehouse == nil {
		t.Fatal("Treehouse = nil, want the pool slot")
	}
	if loc.Treehouse.Pool != "widget-abc123" {
		t.Errorf("Pool = %q, want %q", loc.Treehouse.Pool, "widget-abc123")
	}
	if loc.Treehouse.Slot != "3" {
		t.Errorf("Slot = %q, want %q", loc.Treehouse.Slot, "3")
	}
	if !loc.Treehouse.Leased {
		t.Error("Leased = false, want true")
	}
	if loc.Treehouse.LeaseHolder != "agent:build-the-thing" {
		t.Errorf("LeaseHolder = %q, want %q", loc.Treehouse.LeaseHolder, "agent:build-the-thing")
	}
	if loc.Repo == nil || !loc.Repo.IsWorktree {
		t.Error("a pool slot should also be reported as a linked worktree")
	}
}

// Sitting under a pool directory without being one of its registered slots is
// an ordinary checkout, not a pool worktree.
func TestDetectUnregisteredPathUnderPool(t *testing.T) {
	base := t.TempDir()
	main := initRepo(t, filepath.Join(base, "widget"))

	pool := filepath.Join(base, "pool", "widget-abc123")
	stray := filepath.Join(pool, "9", "widget")
	addWorktree(t, main, stray)
	writeManifest(t, pool, map[string]any{
		"worktrees": []any{
			map[string]any{"name": "1", "path": filepath.Join(pool, "1", "widget")},
		},
	})

	loc := mustDetect(t, stray)
	if loc.Treehouse != nil {
		t.Errorf("Treehouse = %+v, want nil for a path the manifest does not list", loc.Treehouse)
	}
	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the checkout")
	}
}

// A pool worktree checked out detached is the normal treehouse state, so the
// branch is empty while Detached is set.
func TestDetectDetachedHead(t *testing.T) {
	base := t.TempDir()
	main := initRepo(t, filepath.Join(base, "widget"))
	linked := filepath.Join(base, "detached", "widget")
	head := git(t, main, "rev-parse", "HEAD")
	git(t, main, "worktree", "add", "--detach", linked, head)

	loc := mustDetect(t, linked)
	if loc.Repo == nil {
		t.Fatal("Repo = nil")
	}
	if !loc.Repo.Detached {
		t.Error("Detached = false, want true")
	}
	if loc.Repo.Branch != "" {
		t.Errorf("Branch = %q, want empty on a detached head", loc.Repo.Branch)
	}
	if loc.Repo.Head != head {
		t.Errorf("Head = %q, want %q", loc.Repo.Head, head)
	}
}

func mustDetect(t *testing.T, dir string) *detect.Location {
	t.Helper()
	loc, err := detect.New().Detect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Detect(%s): %v", dir, err)
	}
	if loc == nil {
		t.Fatalf("Detect(%s) returned nil location", dir)
	}
	return loc
}

// initRepo makes a repo with one commit.
func initRepo(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	git(t, path, "init", "-q", "-b", "main")
	git(t, path, "config", "user.email", "test@example.com")
	git(t, path, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git(t, path, "add", "README.md")
	git(t, path, "commit", "-q", "-m", "initial")
	return path
}

func addWorktree(t *testing.T, repo, path string) {
	t.Helper()
	git(t, repo, "worktree", "add", "-q", "-b", filepath.Base(filepath.Dir(path))+"-work", path)
}

func writeManifest(t *testing.T, pool string, state map[string]any) {
	t.Helper()
	if err := os.MkdirAll(pool, 0o755); err != nil {
		t.Fatalf("mkdir pool: %v", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pool, "treehouse-state.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return trimNewline(string(out))
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// norm mirrors the path normalization the detector applies, so tests compare
// like with like on a platform where the temp directory is a symlink.
func norm(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return resolved
}
