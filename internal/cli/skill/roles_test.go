package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

const roomDir = "/src/multiplayer"

func roleFixture(t *testing.T) (*sqlitestore.Store, *session.Session) {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	sess := &session.Session{
		ID: "holder", Harness: session.HarnessClaude, Status: session.StatusActive,
		Place:     session.Place{CWD: roomDir, Repo: &session.Repo{Name: "multiplayer", Root: roomDir, MainRoot: roomDir}},
		StartedAt: now, LastSeen: now,
	}
	if err := st.Upsert(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return st, sess
}

func assign(t *testing.T, st store.RoleStore, key, name, desc string) {
	t.Helper()
	r := &store.Role{SessionKey: key, Room: roomDir, Name: name, Description: desc, AssignedAt: time.Now()}
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

func roleSkillPaths(home, alias string) []string {
	var out []string
	for _, spec := range harness.Specs() {
		out = append(out, filepath.Join(home, spec.SkillsDir, RolePrefix+alias, "SKILL.md"))
	}
	return out
}

func TestSyncRolesWritesUpdatesAndRemoves(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, sess := roleFixture(t)
	assign(t, st, sess.Key(), "tester", "Runs the suite. Send finished changes.")

	syncRoles(t, home, st)
	for _, path := range roleSkillPaths(home, sess.Alias) {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("role skill missing: %v", err)
		}
		for _, want := range []string{
			"name: crew-role-" + sess.Alias + "\n",
			sess.Alias + " holds the tester role in room multiplayer (worktree).",
			"> Runs the suite. Send finished changes.",
			"crew post request --to " + sess.Alias + " \"<task, scope, expected result>\"",
			"If you are " + sess.Alias + ", this is your role",
		} {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s is missing %q:\n%s", path, want, got)
			}
		}
	}
	// A second run changes nothing.
	syncRoles(t, home, st)

	assign(t, st, sess.Key(), "reviewer", "Reviews diffs.")
	syncRoles(t, home, st)
	for _, path := range roleSkillPaths(home, sess.Alias) {
		if got, _ := os.ReadFile(path); !strings.Contains(string(got), "the reviewer role") {
			t.Errorf("role skill not updated:\n%s", got)
		}
	}

	if err := st.End(ctx, sess.Key(), time.Now(), "exit"); err != nil {
		t.Fatal(err)
	}
	syncRoles(t, home, st)
	for _, path := range roleSkillPaths(home, sess.Alias) {
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Errorf("stale role skill %s is still there", path)
		}
	}
	if left, err := st.Roles(ctx, store.RoleFilter{}); err != nil || len(left) != 0 {
		t.Errorf("roles after the session ended = (%v, %v), want none", left, err)
	}
}

// A role skill you edited, and any directory crew did not write, are yours.
func TestSyncRolesLeavesEditedAndForeignSkills(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, sess := roleFixture(t)
	assign(t, st, sess.Key(), "tester", "Runs the suite.")
	syncRoles(t, home, st)

	skills := filepath.Join(home, ".claude", "skills")
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
	assign(t, st, sess.Key(), "reviewer", "Reviews diffs.")
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
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", RolePrefix+sess.Alias)); !os.IsNotExist(err) {
		t.Error("unedited codex role skill is still there")
	}
}

func TestRoleDocQuotesDescription(t *testing.T) {
	r := &store.Role{Name: "tester", Description: `Runs "go test": reports file:line, C:\ paths`}
	got := string(roleDoc("moss-otter", r, roomOf(&session.Session{}, roomDir)))
	want := `description: "moss-otter holds the tester role in crew room multiplayer: Runs \"go test\": reports file:line, C:\\ paths. In that room, send matching work with crew post request --to moss-otter."` + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("front matter description is not quoted as YAML:\n%s", got)
	}
	if !strings.HasPrefix(got, "---\nname: crew-role-moss-otter\ndescription: ") {
		t.Errorf("front matter:\n%s", got)
	}
}

func TestRemoveRoles(t *testing.T) {
	home := t.TempDir()
	st, sess := roleFixture(t)
	assign(t, st, sess.Key(), "tester", "Runs the suite.")
	syncRoles(t, home, st)
	skills := filepath.Join(home, ".claude", "skills")
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
