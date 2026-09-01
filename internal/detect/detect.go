// Package detect resolves a working directory into the git checkout, and the
// worktree pool, that own it.
package detect

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// Location is everything the registry can learn about where a session runs.
// Repo is nil outside a git checkout; Pool is nil unless that checkout is a
// worktree lent out by a pool manager.
type Location struct {
	CWD  string
	Repo *session.Repo
	Pool *session.Pool
}

// Detector resolves a working directory into a Location. The hook depends on
// this interface rather than the concrete implementation so tests can supply a
// fixed location without a git binary or a real pool on disk.
type Detector interface {
	Detect(ctx context.Context, cwd string) (*Location, error)
}

// detectBudget caps detection as a whole rather than each git call, so a wedged
// git cannot eat the hook's budget one subprocess at a time.
const detectBudget = 4 * time.Second

// Local detects against the real filesystem and the git binary on PATH.
// Providers defaults to every worktree manager this package knows.
type Local struct {
	Providers []Provider
}

func New() *Local { return &Local{Providers: providers} }

// Detect resolves cwd. Not being a git checkout is not an error, and unreadable
// pool state is swallowed: a session is worth recording either way.
func (l *Local) Detect(ctx context.Context, cwd string) (*Location, error) {
	ctx, cancel := context.WithTimeout(ctx, detectBudget)
	defer cancel()

	loc := &Location{CWD: normalize(cwd)}

	repo, err := gitRepo(ctx, cwd)
	if err != nil {
		return loc, err
	}
	if repo == nil {
		return loc, nil
	}
	loc.Repo = repo

	loc.Pool = l.pool(repo.Root)
	return loc, nil
}

// normalize makes a path absolute and resolves symlinks so that paths coming
// from different sources - a hook payload, a pool manifest, git output -
// compare equal. On macOS this collapses /var against /private/var.
func normalize(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// The path may not exist yet; an absolute clean path is still useful.
		return abs
	}
	return resolved
}

// hostname returns the machine name, or "unknown" if it cannot be read.
func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

func Hostname() string { return hostname() }
