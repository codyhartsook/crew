package skill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

// Claude reads skills from its cwd, Codex from the git root.
const (
	claudeSkillsDir = ".claude/skills"
	codexSkillsDir  = ".agents/skills"
)

// ClaudeDir is where a Claude session in cwd reads project skills.
func ClaudeDir(cwd string) string { return filepath.Join(cwd, claudeSkillsDir) }

// SyncRoles writes role skills into their rooms and clears stale ones. Idempotent.
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
	recorded, err := rs.SkillDirs(ctx)
	if err != nil {
		return err
	}

	var errs []error
	desired, dropped := desiredSkills(roles, active)
	for _, key := range dropped {
		if _, err := rs.Drop(ctx, key); err != nil {
			errs = append(errs, err)
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(desired)) {
		// Exclude, record, then write: never in git status, always found again.
		if err := ensureExcluded(dir); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := rs.RecordSkillDir(ctx, dir); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, writeSkills(dir, desired[dir]))
	}

	sweep := slices.Clone(recorded)
	for _, spec := range harness.Specs() {
		sweep = append(sweep, spec.SkillsPath(home))
	}
	slices.Sort(sweep)
	for _, dir := range slices.Compact(sweep) {
		err := removeStale(dir, desired[dir])
		errs = append(errs, err)
		if err == nil && len(desired[dir]) == 0 && slices.Contains(recorded, dir) {
			errs = append(errs, rs.ForgetSkillDir(ctx, dir))
		}
	}
	return errors.Join(errs...)
}

// desiredSkills maps skills dirs to their role skills and lists ended roles.
func desiredSkills(roles []*store.Role, active []*session.Session) (map[string]map[string]roleSkill, []string) {
	byKey := make(map[string]*session.Session, len(active))
	for _, s := range active {
		byKey[s.Key()] = s
	}
	desired := map[string]map[string]roleSkill{}
	add := func(dir, name string, want roleSkill) {
		if desired[dir] == nil {
			desired[dir] = map[string]roleSkill{}
		}
		desired[dir][name] = want
	}
	var dropped []string
	for _, r := range roles {
		s, ok := byKey[r.SessionKey]
		if !ok {
			dropped = append(dropped, r.SessionKey)
			continue
		}
		rm := roomOf(s, r.Room)
		// A repo room has no single directory.
		if s.Alias == "" || rm.Scope == room.ScopeRepo {
			continue
		}
		root, _, _ := strings.Cut(r.Room, "#")
		if !filepath.IsAbs(root) {
			continue
		}
		name := RolePrefix + s.Alias
		want := roleSkill{roleDoc(s.Alias, r, rm), r.SessionKey}
		add(filepath.Join(root, codexSkillsDir), name, want)
		for _, m := range active {
			// Local rooms only: a sibling worktree's repo room shares the main checkout's key.
			if m.Harness == session.HarnessClaude && m.CWD != "" && slices.ContainsFunc(room.For(m.Place), func(x room.Room) bool { return x.Key == r.Room && x.Scope.Local() }) {
				add(ClaudeDir(m.CWD), name, want)
			}
		}
	}
	return desired, dropped
}

// writeSkills writes missing or changed skills, skipping ones you edited.
func writeSkills(dir string, skills map[string]roleSkill) error {
	var errs []error
	for name, want := range skills {
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
	return errors.Join(errs...)
}

// removeStale deletes our unedited role skills in dir that keep doesn't name.
func removeStale(dir string, keep map[string]roleSkill) error {
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	var errs []error
	for _, e := range entries {
		if _, ok := keep[e.Name()]; ok || !e.IsDir() || !strings.HasPrefix(e.Name(), RolePrefix) {
			continue
		}
		if _, err := removeOurs(filepath.Join(dir, e.Name()), false); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// excludeLines keep role skills out of git status in any checkout.
var excludeLines = []string{"**/.claude/skills/crew-role-*/", "**/.agents/skills/crew-role-*/"}

// ensureExcluded adds excludeLines to the shared exclude of the repo holding dir.
func ensureExcluded(dir string) error {
	common, err := gitCommonDir(dir)
	if err != nil || common == "" {
		return err
	}
	path := filepath.Join(common, "info", "exclude")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	have := strings.Split(string(existing), "\n")
	var add strings.Builder
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		add.WriteString("\n")
	}
	missing := false
	for _, line := range excludeLines {
		if !slices.ContainsFunc(have, func(h string) bool { return strings.TrimSpace(h) == line }) {
			add.WriteString(line + "\n")
			missing = true
		}
	}
	if !missing {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("exclude role skills: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("exclude role skills: %w", err)
	}
	_, err = f.WriteString(add.String())
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("exclude role skills: %w", err)
	}
	return nil
}

// gitCommonDir finds the repo's shared git dir for dir, or "" outside git.
func gitCommonDir(dir string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		dotGit := filepath.Join(d, ".git")
		info, err := os.Stat(dotGit)
		switch {
		case err == nil && info.IsDir():
			return dotGit, nil
		case err == nil:
			return commonFromGitFile(d, dotGit)
		case !os.IsNotExist(err):
			return "", fmt.Errorf("find git dir for %s: %w", dir, err)
		}
		if filepath.Dir(d) == d {
			return "", nil
		}
	}
}

// commonFromGitFile follows a worktree's "gitdir:" file and its commondir.
func commonFromGitFile(workTree, dotGit string) (string, error) {
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dotGit, err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("%s is not a gitdir file", dotGit)
	}
	gitDir = resolve(workTree, strings.TrimSpace(gitDir))
	common, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if os.IsNotExist(err) {
		return gitDir, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", gitDir, err)
	}
	return resolve(gitDir, strings.TrimSpace(string(common))), nil
}

func resolve(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
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
		"resolve each one with crew resolve.\n", alias)
	return []byte(b.String())
}
