// Package detect resolves a working directory into the git checkout and
// treehouse pool worktree that own it.
package detect

import (
	"context"
	"os"
	"path/filepath"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// Location is everything the registry can learn about where a session runs.
// Repo is nil outside a git checkout; Treehouse is nil unless that checkout is
// a worktree drawn from a treehouse pool.
type Location struct {
	CWD       string
	Repo      *session.Repo
	Treehouse *session.Treehouse
}

// Detector resolves a working directory into a Location. The hook depends on
// this interface rather than the concrete implementation so tests can supply a
// fixed location without a git binary or a real pool on disk.
type Detector interface {
	Detect(ctx context.Context, cwd string) (*Location, error)
}

// Local detects against the real filesystem and the git binary on PATH.
type Local struct{}

func New() *Local { return &Local{} }

// Detect resolves cwd. Not being a git checkout is not an error, and unreadable
// treehouse state is swallowed: a session is worth recording either way.
func (l *Local) Detect(ctx context.Context, cwd string) (*Location, error) {
	loc := &Location{CWD: normalize(cwd)}

	repo, err := gitRepo(ctx, cwd)
	if err != nil {
		return loc, err
	}
	if repo == nil {
		return loc, nil
	}
	loc.Repo = repo

	if th, err := treehouseFor(repo.Root); err == nil {
		loc.Treehouse = th
	}
	return loc, nil
}

// normalize makes a path absolute and resolves symlinks so that paths coming
// from different sources - a hook payload, a treehouse state file, git output -
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
