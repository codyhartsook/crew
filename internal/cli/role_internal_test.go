package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// roleWorld registers codex sessions in this checkout's room under a temp
// HOME and database, identifies the caller as the first, and returns them.
func roleWorld(t *testing.T, ids ...string) (string, string, []*session.Session) {
	t.Helper()
	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	loc, err := detect.New().Detect(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("HOME", home)
	t.Setenv("CREW_DB", path)
	clearSessionEnv(t)
	t.Setenv("CODEX_THREAD_ID", ids[0])

	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var out []*session.Session
	for _, id := range ids {
		sess := &session.Session{ID: id, Harness: session.HarnessCodex, Status: session.StatusActive, Place: *loc, StartedAt: time.Now(), LastSeen: time.Now()}
		if err := st.Upsert(ctx, sess); err != nil {
			t.Fatal(err)
		}
		out = append(out, sess)
	}
	return home, path, out
}

// clearSessionEnv hides the harness running the tests, so only the identity a
// test sets is seen.
func clearSessionEnv(t *testing.T) {
	t.Helper()
	for _, spec := range harness.Specs() {
		for _, env := range spec.SessionEnv {
			t.Setenv(env, "")
		}
	}
}

func roleSkillExists(home, alias string) (claude, codex bool) {
	_, errClaude := os.Stat(filepath.Join(home, ".claude", "skills", "crew-role-"+alias, "SKILL.md"))
	_, errCodex := os.Stat(filepath.Join(home, ".codex", "skills", "crew-role-"+alias, "SKILL.md"))
	return errClaude == nil, errCodex == nil
}

func TestRoleCommandPublishesAndDropsSkills(t *testing.T) {
	home, path, sessions := roleWorld(t, "holder")
	alias := sessions[0].Alias

	if err := run(t, "role", "Tester", "Runs the suite."); err == nil || !strings.Contains(err.Error(), "lowercase") {
		t.Fatalf("invalid name error = %v", err)
	}
	if err := run(t, "role", "tester", "Runs the suite and reports failures with file:line."); err != nil {
		t.Fatalf("role: %v", err)
	}
	if claude, codex := roleSkillExists(home, alias); !claude || !codex {
		t.Fatalf("role skill written = claude %v, codex %v, want both", claude, codex)
	}
	got, err := os.ReadFile(filepath.Join(home, ".codex", "skills", "crew-role-"+alias, "SKILL.md"))
	if err != nil || !strings.Contains(string(got), `"`+alias+` holds the tester role in crew room `) {
		t.Errorf("role skill = (%s, %v)", got, err)
	}

	if err := run(t, "role", "reviewer", "Reviews diffs."); err != nil {
		t.Fatalf("replace: %v", err)
	}
	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := st.Roles(context.Background(), store.RoleFilter{})
	st.Close()
	if err != nil || len(roles) != 1 || roles[0].Name != "reviewer" {
		t.Fatalf("roles after replace = (%v, %v), want one reviewer", roles, err)
	}

	if err := run(t, "role", "--drop"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if claude, codex := roleSkillExists(home, alias); claude || codex {
		t.Errorf("role skill after drop = claude %v, codex %v, want gone", claude, codex)
	}
}

func TestRoleNeedsAnIdentifiedAgent(t *testing.T) {
	roleWorld(t, "holder", "other")
	clearSessionEnv(t)
	if err := run(t, "role", "tester", "Runs the suite."); err == nil || !strings.Contains(err.Error(), "could not be identified") {
		t.Fatalf("role from an unidentified caller = %v", err)
	}
}

func TestLsShowsRolesAndMarksCaller(t *testing.T) {
	_, _, sessions := roleWorld(t, "holder", "other")
	if err := run(t, "role", "tester", "Runs the suite."); err != nil {
		t.Fatal(err)
	}

	out, err := runOut(t, "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, sessions[0].Alias+" (you)") || !strings.Contains(out, sessions[1].Alias) {
		t.Fatalf("ls =\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "ROLE") || strings.Contains(lines[0], "DESCRIPTION") {
		t.Errorf("header = %q", lines[0])
	}
	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, sessions[0].Alias):
			if !strings.Contains(line, sessions[0].Alias+" (you)") || !strings.Contains(line, "tester") {
				t.Errorf("caller row = %q, want (you) and its role", line)
			}
		case strings.HasPrefix(line, sessions[1].Alias):
			if strings.Contains(line, "(you)") || strings.Contains(line, "tester") {
				t.Errorf("other row = %q, want no mark and no role", line)
			}
		}
	}

	human, err := runOut(t, "ls", "--human")
	if err != nil || !strings.Contains(human, "Runs the suite.") {
		t.Errorf("ls --human = (%s, %v), want the role description", human, err)
	}
	asJSON, err := runOut(t, "ls", "--json")
	if err != nil || !strings.Contains(asJSON, `"name": "tester"`) {
		t.Errorf("ls --json = (%s, %v), want the role", asJSON, err)
	}

	// A person's shell is nobody's row.
	clearSessionEnv(t)
	if out, err := runOut(t, "ls"); err != nil || strings.Contains(out, "(you)") {
		t.Errorf("ls from a shell = (%s, %v), want no mark", out, err)
	}
}

func TestPostToDepartedAgent(t *testing.T) {
	roleWorld(t, "sender")
	err := run(t, "post", "request", "--to", "gone-otter", "run the suite")
	if err == nil || !strings.Contains(err.Error(), "isn't active in this room") || !strings.Contains(err.Error(), "role it held is free") || !strings.Contains(err.Error(), "crew ls") {
		t.Fatalf("post to a departed agent = %v", err)
	}
}

// runOut is run with the output kept.
func runOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	root := New()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}
