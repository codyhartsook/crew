package hookcmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/routing"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// rosterFor is tier 1 routing: active roles injected as advisory context,
// silent when none are active. Routing is logged once per room, so a
// multi-room session attributes each role to the room that activated it.
func rosterFor(ctx context.Context, st store.Store, sess *session.Session, here []room.Room) (string, error) {
	rs, ok := st.(store.RoleStore)
	if !ok || sess == nil {
		return "", nil
	}
	rl, hasRouting := st.(store.RoutingStore)

	seen := map[string]bool{}
	var names []string
	var logErr error
	for _, r := range here {
		active, err := rs.ActiveRoles(ctx, r.Key)
		if err != nil {
			return "", err
		}
		if len(active) == 0 {
			continue
		}
		roomNames := make([]string, len(active))
		for i, a := range active {
			roomNames[i] = a.Role
			if !seen[a.Role] {
				seen[a.Role] = true
				names = append(names, a.Role)
			}
		}
		if hasRouting {
			d := &routing.Decision{Room: r.Key, Session: sess.Key(), Roles: roomNames, CreatedAt: time.Now().UTC()}
			if err := rl.LogRouting(ctx, d); err != nil {
				logErr = errors.Join(logErr, err)
			}
		}
	}
	if len(names) == 0 {
		return "", logErr
	}
	sort.Strings(names)
	return rosterText(names, discoverRoles(sess)), logErr
}

// activeRoleNames is every role active across here, deduplicated.
func activeRoleNames(ctx context.Context, rs store.RoleStore, here []room.Room) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	for _, r := range here {
		active, err := rs.ActiveRoles(ctx, r.Key)
		if err != nil {
			return nil, err
		}
		for _, a := range active {
			if !seen[a.Role] {
				seen[a.Role] = true
				names = append(names, a.Role)
			}
		}
	}
	return names, nil
}

// defsFor resolves each active name to its definition, dropping any that no
// longer has one - a role can be active with its file since removed.
func defsFor(names []string, reg *role.Registry) []role.Definition {
	if reg == nil {
		return nil
	}
	var defs []role.Definition
	for _, name := range names {
		if def, ok := reg.Get(name); ok {
			defs = append(defs, def)
		}
	}
	return defs
}

// discoverRoles reads role definitions to describe an active roster. A
// failure here (an unreadable directory, a bad definition) is not worth
// failing the hook over: the roster still injects, just by name. The
// session's own Repo is already known, so this skips re-detecting it.
func discoverRoles(sess *session.Session) *role.Registry {
	repoRoot := ""
	if sess.Repo != nil {
		repoRoot = sess.Repo.Root
	}
	reg, err := role.DiscoverFor(repoRoot)
	if err != nil {
		return nil
	}
	return reg
}

func rosterText(names []string, reg *role.Registry) string {
	var b strings.Builder
	b.WriteString("Active roles here:\n")
	for _, name := range names {
		if reg != nil {
			if def, ok := reg.Get(name); ok {
				fmt.Fprintf(&b, "- %s (%s): %s\n", def.Name, def.Harness, def.Description)
				continue
			}
		}
		fmt.Fprintf(&b, "- %s\n", name)
	}
	return strings.TrimRight(b.String(), "\n")
}

// joinContext combines the room briefing and the role roster, either of which
// may be empty, into the one string a hook injects.
func joinContext(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}
