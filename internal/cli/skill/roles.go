package skill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// RolePrefix starts the directory name of every role skill crew manages.
const RolePrefix = "crew-role-"

type roleSkill struct {
	content    []byte
	sessionKey string
}

// SyncRoles makes the role skills under home match active assignments. Idempotent.
func SyncRoles(ctx context.Context, home string, st store.Store) error {
	rs, ok := st.(store.RoleStore)
	if !ok {
		return nil
	}
	// Roles before sessions, so a session that ends in between loses its role
	// rather than one assigned in between being dropped unseen.
	roles, err := rs.Roles(ctx, store.RoleFilter{})
	if err != nil {
		return err
	}
	active, err := st.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return err
	}
	byKey := make(map[string]*session.Session, len(active))
	for _, s := range active {
		byKey[s.Key()] = s
	}

	var errs []error
	desired := map[string]roleSkill{}
	for _, r := range roles {
		s, ok := byKey[r.SessionKey]
		if !ok {
			if _, err := rs.Drop(ctx, r.SessionKey); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if s.Alias == "" {
			continue
		}
		desired[RolePrefix+s.Alias] = roleSkill{roleDoc(s.Alias, r, roomOf(s, r.Room)), r.SessionKey}
	}
	for _, spec := range harness.Specs() {
		errs = append(errs, syncDir(spec.SkillsPath(home), desired))
	}
	return errors.Join(errs...)
}

// syncDir reconciles one skills directory, skipping dirs not ours or edited.
func syncDir(dir string, desired map[string]roleSkill) error {
	var errs []error
	for name, want := range desired {
		path := filepath.Join(dir, name, "SKILL.md")
		existing, err := os.ReadFile(path)
		switch {
		case err == nil && bytes.Equal(existing, want.content):
			if !Ours(path, existing) {
				errs = append(errs, writeMarker(path, existing, want.sessionKey))
			}
			continue
		case err == nil && !Ours(path, existing):
			continue
		case err != nil && !os.IsNotExist(err):
			errs = append(errs, fmt.Errorf("read %s: %w", path, err))
			continue
		}
		errs = append(errs, write(path, want.content, want.sessionKey))
	}

	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("read %s: %w", dir, err))
	}
	for _, e := range entries {
		if _, keep := desired[e.Name()]; keep || !e.IsDir() || !strings.HasPrefix(e.Name(), RolePrefix) {
			continue
		}
		if _, err := removeOurs(filepath.Join(dir, e.Name()), false); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RemoveRoles deletes our unedited role skills in skillsDir and counts them.
func RemoveRoles(skillsDir string, dryRun bool) (int, error) {
	entries, err := os.ReadDir(skillsDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", skillsDir, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), RolePrefix) {
			continue
		}
		removed, err := removeOurs(filepath.Join(skillsDir, e.Name()), dryRun)
		if err != nil {
			return n, err
		}
		if removed {
			n++
		}
	}
	return n, nil
}

// roomOf finds the role's room among the session's, else names it from the key.
func roomOf(s *session.Session, key string) room.Room {
	for _, r := range room.For(s.Place) {
		if r.Key == key {
			return r
		}
	}
	return room.Room{Key: key, Name: room.NameFor(key)}
}

// roleDoc frames the description with holder, room and how to send it work.
func roleDoc(alias string, r *store.Role, rm room.Room) []byte {
	desc := strings.TrimSpace(r.Description)
	if !strings.HasSuffix(desc, ".") && !strings.HasSuffix(desc, "!") && !strings.HasSuffix(desc, "?") {
		desc += "."
	}
	scope := ""
	if rm.Scope != "" {
		scope = " (" + string(rm.Scope) + ")"
	}
	// strconv.Quote's escapes are all valid YAML double-quoted escapes.
	summary := strconv.Quote(fmt.Sprintf("%s holds the %s role in crew room %s: %s In that room, send matching work with crew post request --to %s.",
		alias, r.Name, rm.Name, desc, alias))

	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s%s\ndescription: %s\n---\n\n", RolePrefix, alias, summary)
	fmt.Fprintf(&b, "%s holds the %s role in room %s%s.\n\n> %s\n\n", alias, r.Name, rm.Name, scope, desc)
	fmt.Fprintf(&b, "In that room, when a task matches this role, hand it over and keep your own\n"+
		"context for the work only you can do:\n\n"+
		"    crew post request --to %s \"<task, scope, expected result>\"\n\n", alias)
	fmt.Fprintf(&b, "If you are %s, this is your role: take requests addressed to you, and\n"+
		"resolve each one with crew resolve. Agents in other rooms can ignore this skill.\n", alias)
	return []byte(b.String())
}
