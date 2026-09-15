// Package delegate resolves a role and runs it headless, returning its
// bounded result. Used by both a --wait launcher and the broker.
package delegate

import (
	"context"
	"fmt"
	"os"

	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// Resolve discovers both scopes from cwd and looks up name.
func Resolve(ctx context.Context, name string) (role.Definition, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return role.Definition{}, fmt.Errorf("resolve working directory: %w", err)
	}
	return ResolveIn(ctx, cwd, name)
}

// ResolveIn discovers both scopes from dir rather than cwd, for a caller not
// sitting in the room it resolves for (the broker, running any repo's job).
func ResolveIn(ctx context.Context, dir, name string) (role.Definition, error) {
	reg, err := role.DiscoverFromDir(ctx, dir)
	if err != nil {
		return role.Definition{}, err
	}
	def, ok := reg.Get(name)
	if !ok {
		return role.Definition{}, fmt.Errorf("no role named %q is defined", name)
	}
	return def, nil
}

// PickHarness resolves which harness runs a role; "any" requires flag.
func PickHarness(def role.Definition, flag string) (session.Harness, error) {
	if flag != "" {
		h := session.Harness(flag)
		if h != session.HarnessClaude && h != session.HarnessCodex {
			return "", fmt.Errorf("--harness must be claude or codex, got %q", flag)
		}
		if def.Harness != role.HarnessAny && string(def.Harness) != flag {
			return "", fmt.Errorf("role %q only runs on %s, not %s", def.Name, def.Harness, flag)
		}
		return h, nil
	}
	switch def.Harness {
	case role.HarnessClaude:
		return session.HarnessClaude, nil
	case role.HarnessCodex:
		return session.HarnessCodex, nil
	default:
		return "", fmt.Errorf("role %q allows either harness; pass --harness claude|codex", def.Name)
	}
}
