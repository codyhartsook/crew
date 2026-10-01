// Package detect resolves a working directory into the workspace that owns it:
// a git checkout and the worktree pool that lent it, or an anchored folder.
package detect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// Detector resolves a working directory into a place. Hooks use the interface so
// tests can supply a fixed place without git or a real pool.
type Detector interface {
	Detect(ctx context.Context, cwd string) (*session.Place, error)
}

// detectBudget caps detection as a whole rather than each git call, so a wedged
// git cannot eat the hook's budget one subprocess at a time.
const detectBudget = 4 * time.Second

// Local detects against the real filesystem and the git binary on PATH.
// Anchors and Providers default to everything this package knows.
type Local struct {
	Anchors   []Anchor
	Providers []Provider
}

func New() *Local { return &Local{Anchors: anchors, Providers: providers} }

// Detect resolves cwd. Belonging to no workspace is not an error, and
// unreadable pool state is swallowed: a session is worth recording either way.
func (l *Local) Detect(ctx context.Context, cwd string) (*session.Place, error) {
	ctx, cancel := context.WithTimeout(ctx, detectBudget)
	defer cancel()

	place := &session.Place{CWD: normalize(cwd)}

	for _, a := range l.Anchors {
		found, err := a.Lookup(ctx, cwd)
		switch {
		case errors.Is(err, ErrAnchorUnavailable):
			continue
		case err != nil:
			return place, err
		case found == nil:
			continue
		}
		found.CWD = place.CWD
		// A pool lends out checkouts, so it decorates a repo, never a folder.
		if found.Repo != nil {
			found.Pool = l.pool(found.Repo.Root)
		}
		return found, nil
	}
	return place, nil
}

// normalize makes a path absolute and resolves symlinks so paths from different
// sources compare equal (macOS: /var vs /private/var).
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
