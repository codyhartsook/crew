package detect

import (
	"bytes"
	"context"
	"errors"
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
	// --path-format=absolute matters: without it --git-common-dir is reported
	// relative to the process cwd, so running from a subdirectory yields
	// "../.git" instead of a usable path.
	out, ok, err := runGit(ctx, dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return nil, nil
	}

	root := normalize(strings.TrimSpace(lines[0]))
	commonDir := normalize(strings.TrimSpace(lines[1]))
	mainRoot := filepath.Dir(commonDir)

	repo := &session.Repo{
		Root:       root,
		MainRoot:   mainRoot,
		Name:       filepath.Base(mainRoot),
		IsWorktree: mainRoot != root,
	}

	// A pooled treehouse worktree is normally checked out detached, so an empty
	// branch with Detached set is the common case rather than an anomaly.
	if branch, ok, err := runGit(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
		return nil, err
	} else if ok {
		repo.Branch = branch
	} else {
		repo.Detached = true
	}

	// Empty on a repository with no commits yet.
	if head, _, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil, err
	} else {
		repo.Head = head
	}

	if remote, ok, err := runGit(ctx, dir, "remote", "get-url", "origin"); err != nil {
		return nil, err
	} else if ok {
		repo.Remote = remote
	}

	return repo, nil
}
