package skill

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func openStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// repoPlace is a checkout at root whose repository lives at main.
func repoPlace(cwd, root, main string) session.Place {
	return session.Place{CWD: cwd, Repo: &session.Repo{Name: filepath.Base(main), Root: root, MainRoot: main, IsWorktree: root != main}}
}

func addSession(t *testing.T, st *sqlitestore.Store, id string, h session.Harness, place session.Place) *session.Session {
	t.Helper()
	now := time.Now().UTC()
	sess := &session.Session{ID: id, Harness: h, Status: session.StatusActive, Place: place, StartedAt: now, LastSeen: now}
	if err := st.Upsert(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func assign(t *testing.T, st store.RoleStore, key, roomKey, name, desc string) {
	t.Helper()
	r := &store.Role{SessionKey: key, Room: roomKey, Name: name, Description: desc, AssignedAt: time.Now()}
	if err := st.Assign(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func syncRoles(t *testing.T, home string, st store.Store) {
	t.Helper()
	if err := SyncRoles(context.Background(), home, st); err != nil {
		t.Fatalf("SyncRoles: %v", err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.name=crew", "-c", "user.email=crew@example.com", "-c", "commit.gpgsign=false"}
	out, err := exec.Command("git", append(base, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	return dir
}

// roleSkillPaths are the Codex copy at root and the Claude copy at cwd.
func roleSkillPaths(root, cwd, alias string) []string {
	return []string{
		filepath.Join(root, ".agents", "skills", RolePrefix+alias, "SKILL.md"),
		filepath.Join(cwd, ".claude", "skills", RolePrefix+alias, "SKILL.md"),
	}
}

func assertClean(t *testing.T, dir string) {
	t.Helper()
	if out := gitRun(t, dir, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git status in %s:\n%s", dir, out)
	}
}

func assertExcludedOnce(t *testing.T, gitDir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for _, want := range excludeLines {
		if n := len(slices.DeleteFunc(slices.Clone(lines), func(l string) bool { return l != want })); n != 1 {
			t.Errorf("exclude has %q %d times:\n%s", want, n, data)
		}
	}
}

func TestDesiredSkillPaths(t *testing.T) {
	const app, pool = "/src/app", "/src/pool"
	st := openStore(t)
	holder := addSession(t, st, "holder", session.HarnessCodex, repoPlace(app, app, app))
	addSession(t, st, "root", session.HarnessClaude, repoPlace(app, app, app))
	addSession(t, st, "dup", session.HarnessClaude, repoPlace(app, app, app))
	addSession(t, st, "nested", session.HarnessClaude, repoPlace(app+"/sub", app, app))
	addSession(t, st, "elsewhere", session.HarnessClaude, repoPlace("/src/other", "/src/other", "/src/other"))
	addSession(t, st, "sibling", session.HarnessClaude, repoPlace("/src/app-wt", "/src/app-wt", app))
	pooled := repoPlace(pool, pool, "/src/main")
	pooled.Pool = &session.Pool{LeaseID: "lease1"}
	poolHolder := addSession(t, st, "pooled", session.HarnessCodex, pooled)
	assign(t, st, holder.Key(), app, "tester", "Runs the suite.")
	assign(t, st, poolHolder.Key(), pool+"#lease1", "reviewer", "Reviews diffs.")
	assign(t, st, "codex:gone", app, "writer", "Writes docs.")

	ctx := context.Background()
	roles, err := st.Roles(ctx, store.RoleFilter{})
	if err != nil {
		t.Fatal(err)
	}
	active, err := st.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	desired, dropped := desiredSkills(roles, active)

	want := map[string][]string{
		app + "/.agents/skills":     {RolePrefix + holder.Alias},
		app + "/.claude/skills":     {RolePrefix + holder.Alias},
		app + "/sub/.claude/skills": {RolePrefix + holder.Alias},
		pool + "/.agents/skills":    {RolePrefix + poolHolder.Alias},
	}
	if len(desired) != len(want) {
		t.Errorf("desired dirs = %v, want %v", desired, want)
	}
	for dir, names := range want {
		got := desired[dir]
		if len(got) != len(names) {
			t.Errorf("%s holds %v, want %v", dir, got, names)
		}
		for _, name := range names {
			if _, ok := got[name]; !ok {
				t.Errorf("%s is missing %s", dir, name)
			}
		}
	}
	if !slices.Equal(dropped, []string{"codex:gone"}) {
		t.Errorf("dropped = %v, want the ended holder", dropped)
	}
}

func TestSyncRolesWritesUpdatesAndRemoves(t *testing.T) {
	ctx := context.Background()
	home, repo := t.TempDir(), gitInit(t)
	st := openStore(t)
	sess := addSession(t, st, "holder", session.HarnessClaude, repoPlace(repo, repo, repo))
	assign(t, st, sess.Key(), repo, "tester", "Runs the suite. Send finished changes.")

	syncRoles(t, home, st)
	for _, path := range roleSkillPaths(repo, repo, sess.Alias) {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("role skill missing: %v", err)
		}
		for _, want := range []string{
			"name: crew-role-" + sess.Alias + "\n",
			sess.Alias + " holds the tester role in room " + filepath.Base(repo) + " (worktree).",
			"> Runs the suite. Send finished changes.",
			"crew post request --to " + sess.Alias + " \"<task, scope, expected result>\"",
			"If you are " + sess.Alias + ", this is your role",
		} {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s is missing %q:\n%s", path, want, got)
			}
		}
	}
	// A second run changes nothing and adds no exclude lines.
	syncRoles(t, home, st)
	assertExcludedOnce(t, filepath.Join(repo, ".git"))
	assertClean(t, repo)
	for _, spec := range harness.Specs() {
		if entries, _ := os.ReadDir(spec.SkillsPath(home)); len(entries) != 0 {
			t.Errorf("global %s dir has %v", spec.Harness, entries)
		}
	}

	assign(t, st, sess.Key(), repo, "reviewer", "Reviews diffs.")
	syncRoles(t, home, st)
	for _, path := range roleSkillPaths(repo, repo, sess.Alias) {
		if got, _ := os.ReadFile(path); !strings.Contains(string(got), "the reviewer role") {
			t.Errorf("role skill not updated:\n%s", got)
		}
	}

	if err := st.End(ctx, sess.Key(), time.Now(), "exit"); err != nil {
		t.Fatal(err)
	}
	syncRoles(t, home, st)
	for _, path := range roleSkillPaths(repo, repo, sess.Alias) {
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Errorf("stale role skill %s is still there", path)
		}
	}
	if left, err := st.Roles(ctx, store.RoleFilter{}); err != nil || len(left) != 0 {
		t.Errorf("roles after the session ended = (%v, %v), want none", left, err)
	}
	if dirs, err := st.SkillDirs(ctx); err != nil || len(dirs) != 0 {
		t.Errorf("recorded dirs after cleanup = (%v, %v), want none", dirs, err)
	}
}

// A worktree shares its repository's exclude file, and a sibling checkout sees nothing.
func TestSyncRolesWorktreeUsesCommonExclude(t *testing.T) {
	home, repo := t.TempDir(), gitInit(t)
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-q", wt)
	st := openStore(t)
	sess := addSession(t, st, "holder", session.HarnessClaude, repoPlace(wt, wt, repo))
	assign(t, st, sess.Key(), wt, "tester", "Runs the suite.")

	syncRoles(t, home, st)
	syncRoles(t, home, st)
	for _, path := range roleSkillPaths(wt, wt, sess.Alias) {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("role skill missing in the worktree: %v", err)
		}
	}
	for _, path := range roleSkillPaths(repo, repo, sess.Alias) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("sibling checkout got %s", path)
		}
	}
	assertExcludedOnce(t, filepath.Join(repo, ".git"))
	assertClean(t, wt)
	assertClean(t, repo)
}

// A sandbox that protects .git must not leave a skill showing in git status.
func TestSyncRolesSkipsDirWhenExcludeFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	home, repo := t.TempDir(), gitInit(t)
	exclude := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exclude, 0o444); err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	sess := addSession(t, st, "holder", session.HarnessClaude, repoPlace(repo, repo, repo))
	assign(t, st, sess.Key(), repo, "tester", "Runs the suite.")

	if err := SyncRoles(context.Background(), home, st); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("SyncRoles = %v, want a permission error", err)
	}
	for _, path := range roleSkillPaths(repo, repo, sess.Alias) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("skill written without an exclude: %s", path)
		}
	}
	if dirs, _ := st.SkillDirs(context.Background()); len(dirs) != 0 {
		t.Errorf("skipped dirs were recorded: %v", dirs)
	}
}

// Role skills an older crew wrote to the global dirs go on the next sync.
func TestSyncRolesRemovesLegacyGlobalSkills(t *testing.T) {
	home := t.TempDir()
	st := openStore(t)
	var legacy []string
	for _, spec := range harness.Specs() {
		path := filepath.Join(spec.SkillsPath(home), RolePrefix+"old-otter", "SKILL.md")
		if err := write(path, []byte("old role"), "claude:old"); err != nil {
			t.Fatal(err)
		}
		legacy = append(legacy, path)
	}
	foreign := filepath.Join(home, ".claude", "skills", RolePrefix+"mine", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	syncRoles(t, home, st)
	for _, path := range legacy {
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Errorf("legacy role skill %s is still there", path)
		}
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("a role skill crew did not write was removed: %v", err)
	}
}

// A role skill you edited, and any directory crew did not write, are yours.
func TestSyncRolesLeavesEditedAndForeignSkills(t *testing.T) {
	ctx := context.Background()
	home, repo := t.TempDir(), gitInit(t)
	st := openStore(t)
	sess := addSession(t, st, "holder", session.HarnessClaude, repoPlace(repo, repo, repo))
	assign(t, st, sess.Key(), repo, "tester", "Runs the suite.")
	syncRoles(t, home, st)

	skills := filepath.Join(repo, ".claude", "skills")
	edited := filepath.Join(skills, RolePrefix+sess.Alias, "SKILL.md")
	if err := os.WriteFile(edited, []byte("my tuning"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(skills, RolePrefix+"someone", "SKILL.md")
	other := filepath.Join(skills, "my-skill", "SKILL.md")
	for _, path := range []string{foreign, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("not crew's"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The edit survives a changed assignment and the session ending.
	assign(t, st, sess.Key(), repo, "reviewer", "Reviews diffs.")
	syncRoles(t, home, st)
	if got, _ := os.ReadFile(edited); string(got) != "my tuning" {
		t.Errorf("edited role skill was rewritten: %q", got)
	}
	if err := st.End(ctx, sess.Key(), time.Now(), "exit"); err != nil {
		t.Fatal(err)
	}
	syncRoles(t, home, st)
	for _, path := range []string{edited, foreign, other} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", path, err)
		}
	}
	// Codex's copy was unedited, so it went.
	if _, err := os.Stat(filepath.Join(repo, ".agents", "skills", RolePrefix+sess.Alias)); !os.IsNotExist(err) {
		t.Error("unedited codex role skill is still there")
	}
}

func TestRoleDocQuotesDescription(t *testing.T) {
	r := &store.Role{Name: "tester", Description: `Runs "go test": reports file:line, C:\ paths`}
	got := string(roleDoc("moss-otter", r, roomOf(&session.Session{}, "/src/multiplayer")))
	want := `description: "moss-otter holds the tester role in crew room multiplayer: Runs \"go test\": reports file:line, C:\\ paths. In that room, send matching work with crew post request --to moss-otter."` + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("front matter description is not quoted as YAML:\n%s", got)
	}
	if !strings.HasPrefix(got, "---\nname: crew-role-moss-otter\ndescription: ") {
		t.Errorf("front matter:\n%s", got)
	}
}

func TestRemoveRoles(t *testing.T) {
	home, repo := t.TempDir(), gitInit(t)
	st := openStore(t)
	sess := addSession(t, st, "holder", session.HarnessClaude, repoPlace(repo, repo, repo))
	assign(t, st, sess.Key(), repo, "tester", "Runs the suite.")
	syncRoles(t, home, st)
	skills := filepath.Join(repo, ".claude", "skills")
	mine := filepath.Join(skills, RolePrefix+"mine", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if n, err := RemoveRoles(skills, true); err != nil || n != 1 {
		t.Fatalf("dry run = (%d, %v), want 1", n, err)
	}
	if n, err := RemoveRoles(skills, false); err != nil || n != 1 {
		t.Fatalf("remove = (%d, %v), want 1", n, err)
	}
	if _, err := os.Stat(filepath.Join(skills, RolePrefix+sess.Alias)); !os.IsNotExist(err) {
		t.Error("crew's role skill is still there")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("a role skill crew did not write was removed")
	}
}
