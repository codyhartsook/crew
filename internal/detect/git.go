package detect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// gitTimeout bounds each git subprocess. A session hook runs in front of the
// user, so a wedged git must fail fast rather than stall the harness.
const gitTimeout = 3 * time.Second

// ErrGitMissing reports that no git binary is on PATH.
var ErrGitMissing = errors.New("git not found on PATH")

// runGit runs git in dir and returns its trimmed stdout. The second return
// value is false when git exited non-zero, which callers treat as "this
// question has no answer here" rather than as a failure.
func runGit(ctx context.Context, dir string, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// GIT_OPTIONAL_LOCKS=0 keeps the hook from contending for the index lock
	// with the agent that is actively working in this checkout.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err == nil {
		return out, true, nil
	}

	// A killed process reports an ExitError, so only the context distinguishes
	// "git timed out" from "git answered no" - and the latter would record the
	// session as if it were outside any checkout.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", false, fmt.Errorf("git %s: %w", args[0], ctxErr)
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, false, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", false, ErrGitMissing
	}
	return "", false, err
}

// gitRepo describes the checkout containing dir. It returns (nil, nil) when dir
// is not inside a git working tree, which is an ordinary outcome: agents are
// often started somewhere that is not a repo.
func gitRepo(ctx context.Context, dir string) (*session.Repo, error) {
	// --path-format=absolute matters: without it the git dirs are reported
	// relative to the process cwd, so running from a subdirectory yields
	// "../.git" instead of a usable path.
	out, ok, err := runGit(ctx, dir, "rev-parse", "--path-format=absolute",
		"--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		return nil, nil
	}

	root := normalize(strings.TrimSpace(lines[0]))
	gitDir := normalize(strings.TrimSpace(lines[1]))
	commonDir := normalize(strings.TrimSpace(lines[2]))

	repo := &session.Repo{
		Root: root,
		// The two dirs differ exactly in a linked worktree, whatever the
		// layout. Comparing paths instead misreads submodules and
		// --separate-git-dir checkouts as worktrees.
		IsWorktree: gitDir != commonDir,
	}
	repo.MainRoot = mainRootFrom(root, commonDir, repo.IsWorktree)
	repo.Name = repoName(repo.MainRoot)

	// A pooled worktree is often checked out detached, so an empty branch with
	// Detached set is a common case rather than an anomaly.
	if branch, ok, err := runGit(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
		return nil, err
	} else if ok {
		repo.Branch = branch
	} else {
		repo.Detached = true
	}

	// Empty on a repository with no commits yet.
	head, _, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return nil, err
	}
	repo.Head = head

	if remote, ok, err := runGit(ctx, dir, "remote", "get-url", "origin"); err != nil {
		return nil, err
	} else if ok {
		repo.Remote = remote
	}

	return repo, nil
}

// mainRootFrom identifies the repository a worktree belongs to. Normally that
// is the checkout holding .git; a bare host or --separate-git-dir has no
// primary checkout, so the common dir stands in for it.
func mainRootFrom(root, commonDir string, isWorktree bool) string {
	if !isWorktree {
		return root
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir)
	}
	return commonDir
}

// repoName labels the repository, dropping the .git suffix a bare or detached
// git dir carries.
func repoName(mainRoot string) string {
	return strings.TrimSuffix(filepath.Base(mainRoot), ".git")
}
