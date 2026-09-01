package detect

import "github.com/codyhartsook/multiplayer/internal/session"

// Provider recognizes a checkout as a worktree lent out by a worktree manager.
// Supporting another manager is a new implementation, not a change to the model.
type Provider interface {
	// Name is the manager this provider speaks for.
	Name() string
	// Lookup returns the pool that owns worktreeRoot, or nil for a checkout the
	// manager does not know about.
	Lookup(worktreeRoot string) (*session.Pool, error)
}

// providers are tried in order, first match winning.
var providers = []Provider{treehouse{}}

// pool asks each provider about the checkout. A provider that cannot read its
// own state is skipped: a session is worth recording either way.
func (l *Local) pool(worktreeRoot string) *session.Pool {
	for _, p := range l.Providers {
		pool, err := p.Lookup(worktreeRoot)
		if err == nil && pool != nil {
			return pool
		}
	}
	return nil
}
