package hookcmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// rosterFor injects active delegation agents across the session's rooms.
func rosterFor(ctx context.Context, st store.Store, sess *session.Session, here []room.Room) (string, error) {
	rs, ok := st.(store.RoleStore)
	if !ok || sess == nil {
		return "", nil
	}

	seen := map[string]bool{}
	var names []string
	for _, r := range here {
		active, err := rs.ActiveRoles(ctx, r.Key)
		if err != nil {
			return "", err
		}
		if len(active) == 0 {
			continue
		}
		for _, a := range active {
			if !seen[a.Role] {
				seen[a.Role] = true
				names = append(names, a.Role)
			}
		}
	}
	if len(names) == 0 {
		return "", nil
	}
	sort.Strings(names)
	return rosterText(names, discoverRoles(sess)), nil
}

// discoverRoles reads definitions for the roster. A failure is not worth
// failing the hook over: the roster still injects, just by name.
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
	b.WriteString("Managing your context is important. The following delegation agents can be spawned to handle self-contained work in a separate context and return a focused result.\n\n")
	b.WriteString("Delegate early when investigation, testing, or another bounded task would consume substantial context here:\n\n")
	b.WriteString("`crew delegate <role> \"<task with scope and expected result>\"`\n\n")
	b.WriteString("Available delegation agents:\n")
	for _, name := range names {
		if reg != nil {
			if def, ok := reg.Get(name); ok {
				fmt.Fprintf(&b, "- %s (%s): %s\n", def.Name, def.Harness, def.Description)
				continue
			}
		}
		fmt.Fprintf(&b, "- %s\n", name)
	}
	b.WriteString("\nKeep tightly coupled implementation and integration decisions in this session.")
	return strings.TrimRight(b.String(), "\n")
}

// joinContext combines the briefing and roster into one injected string.
func joinContext(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}
