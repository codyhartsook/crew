package detect_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/session"
)

func TestDetectOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	loc := mustDetect(t, dir)

	if loc.Repo != nil {
		t.Errorf("Repo = %+v, want nil outside a checkout", loc.Repo)
	}
	if loc.Pool != nil {
		t.Errorf("Pool = %+v, want nil", loc.Pool)
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
	if loc.Pool != nil {
		t.Errorf("Pool = %+v, want nil for an ordinary checkout", loc.Pool)
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
	if loc.Pool != nil {
		t.Errorf("Pool = %+v, want nil for a worktree outside a pool", loc.Pool)
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
	if loc.Pool == nil {
		t.Fatal("Pool = nil, want the pool slot")
	}
	if loc.Pool.Name != "widget-abc123" {
		t.Errorf("Name = %q, want %q", loc.Pool.Name, "widget-abc123")
	}
	if loc.Pool.Slot != "3" {
		t.Errorf("Slot = %q, want %q", loc.Pool.Slot, "3")
	}
	if !loc.Pool.Leased {
		t.Error("Leased = false, want true")
	}
	if loc.Pool.LeaseHolder != "agent:build-the-thing" {
		t.Errorf("LeaseHolder = %q, want %q", loc.Pool.LeaseHolder, "agent:build-the-thing")
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
	if loc.Pool != nil {
		t.Errorf("Pool = %+v, want nil for a path the manifest does not list", loc.Pool)
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

// A hung git must be an error, not "not a git checkout": a location-less
// session falls back to a room keyed on the working directory.
func TestDetectHungGitIsAnError(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "widget"))
	shimHungGit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	loc, err := detect.New().Detect(ctx, repo)
	if err == nil {
		t.Fatal("Detect returned a nil error for a git that never answers")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if loc != nil && loc.Repo != nil {
		t.Errorf("Repo = %+v, want nil when git could not be asked", loc.Repo)
	}
}

// A submodule keeps its git dir under the superproject. Naming the repo from the
// common dir's parent made sibling submodules share one room.
func TestDetectSubmodule(t *testing.T) {
	base := t.TempDir()
	lib := initRepo(t, filepath.Join(base, "lib"))
	super := initRepo(t, filepath.Join(base, "super"))
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	git(t, super, "commit", "-qm", "add submodule")

	loc := mustDetect(t, filepath.Join(super, "vendor", "lib"))
	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the submodule checkout")
	}
	if loc.Repo.IsWorktree {
		t.Error("IsWorktree = true, want false for a submodule")
	}
	if loc.Repo.Name != "lib" {
		t.Errorf("Name = %q, want %q", loc.Repo.Name, "lib")
	}
	if loc.Repo.MainRoot != loc.Repo.Root {
		t.Errorf("MainRoot = %q, want the checkout %q", loc.Repo.MainRoot, loc.Repo.Root)
	}
}

// A worktree of a bare repository: the common dir is the bare repo, not a .git
// inside a checkout, so there is no primary working tree to point at.
func TestDetectWorktreeOfBareRepo(t *testing.T) {
	base := t.TempDir()
	src := initRepo(t, filepath.Join(base, "widget"))
	bare := filepath.Join(base, "widget.git")
	git(t, base, "clone", "-q", "--bare", src, bare)
	linked := filepath.Join(base, "wt")
	git(t, bare, "worktree", "add", "-q", linked, "main")

	loc := mustDetect(t, linked)
	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the linked worktree")
	}
	if !loc.Repo.IsWorktree {
		t.Error("IsWorktree = false, want true")
	}
	if loc.Repo.MainRoot != norm(t, bare) {
		t.Errorf("MainRoot = %q, want the bare repo %q", loc.Repo.MainRoot, norm(t, bare))
	}
	if loc.Repo.Name != "widget" {
		t.Errorf("Name = %q, want %q with the .git suffix dropped", loc.Repo.Name, "widget")
	}
}

// --separate-git-dir moves .git out of the checkout, which does not make the
// checkout a worktree.
func TestDetectSeparateGitDir(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "widget")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	git(t, work, "init", "-q", "-b", "main", "--separate-git-dir="+filepath.Join(base, "widget.git"))
	git(t, work, "config", "user.email", "test@example.com")
	git(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git(t, work, "add", "README.md")
	git(t, work, "commit", "-qm", "initial")

	loc := mustDetect(t, work)
	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the checkout")
	}
	if loc.Repo.IsWorktree {
		t.Error("IsWorktree = true, want false for a primary checkout")
	}
	if loc.Repo.MainRoot != norm(t, work) {
		t.Errorf("MainRoot = %q, want %q", loc.Repo.MainRoot, norm(t, work))
	}
	if loc.Repo.Name != "widget" {
		t.Errorf("Name = %q, want %q", loc.Repo.Name, "widget")
	}
}

// A fresh repo has no HEAD to report, which must not fail detection.
func TestDetectRepoWithoutCommits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	git(t, dir, "init", "-q", "-b", "main")

	loc := mustDetect(t, dir)
	if loc.Repo == nil {
		t.Fatal("Repo = nil, want the checkout")
	}
	if loc.Repo.Head != "" {
		t.Errorf("Head = %q, want empty before the first commit", loc.Repo.Head)
	}
	if loc.Repo.Branch != "main" {
		t.Errorf("Branch = %q, want %q on an unborn branch", loc.Repo.Branch, "main")
	}
}

// Detection is bounded as a whole, so a wedged git cannot burn the hook's
// budget one subprocess at a time.
func TestDetectBoundsTotalTime(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the detect budget")
	}
	repo := initRepo(t, filepath.Join(t.TempDir(), "widget"))
	shimHungGit(t)

	start := time.Now()
	_, err := detect.New().Detect(context.Background(), repo)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Detect returned a nil error for a git that never answers")
	}
	if elapsed > 6*time.Second {
		t.Errorf("Detect took %s, want it bounded by its own budget", elapsed)
	}
}

func mustDetect(t *testing.T, dir string) *session.Place {
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

// shimHungGit puts a git on PATH that never answers. exec'ing sleep means
// killing the child kills the sleep too.
func shimHungGit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakePool answers for any checkout, standing in for a worktree manager other
// than treehouse.
type fakePool struct{}

func (fakePool) Name() string { return "fake" }

func (f fakePool) Lookup(root string) (*session.Pool, error) {
	return &session.Pool{Manager: f.Name(), Name: "somepool", Slot: "7", Root: root}, nil
}

// brokenPool cannot read its own state, which must not stop a later provider.
type brokenPool struct{}

func (brokenPool) Name() string { return "broken" }

func (brokenPool) Lookup(string) (*session.Pool, error) {
	return nil, errors.New("unreadable")
}

// A manager other than treehouse is recognized by registering a provider, with
// no change to the location model.
func TestDetectUsesRegisteredProviders(t *testing.T) {
	repo := initRepo(t, filepath.Join(t.TempDir(), "widget"))

	d := detect.New()
	d.Providers = []detect.Provider{brokenPool{}, fakePool{}}

	loc, err := d.Detect(context.Background(), repo)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if loc.Pool == nil {
		t.Fatal("Pool = nil, want the registered provider's answer")
	}
	if loc.Pool.Manager != "fake" {
		t.Errorf("Manager = %q, want %q past the provider that failed", loc.Pool.Manager, "fake")
	}
	if loc.Pool.Slot != "7" {
		t.Errorf("Slot = %q, want %q", loc.Pool.Slot, "7")
	}
}
