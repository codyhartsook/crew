package hookcmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func writeRoleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const testerRoleTOML = `
name = "tester"
description = "Runs the full test suite and reports failures"
harness = "codex"
memory = "role"
instructions = "Run the suite."
`

func TestRosterInjectsActiveRolesWithDescriptions(t *testing.T) {
	ctx := context.Background()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	writeRoleFile(t, filepath.Join(repo, ".crew", "agents"), "tester.toml", testerRoleTOML)

	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.ActivateRole(ctx, &role.Activation{Room: repo, Role: "tester", ActivatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	sess := &session.Session{
		ID: "s1", Harness: session.HarnessCodex, Status: session.StatusActive,
		Place:     session.Place{CWD: repo, Repo: &session.Repo{Name: "repo", Root: repo, MainRoot: repo}},
		StartedAt: time.Now(), LastSeen: time.Now(),
	}

	out, err := roomsFor(ctx, st, hook.EventStart, sess, sess.Key())
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if !strings.Contains(out, "tester") || !strings.Contains(out, "Runs the full test suite") {
		t.Errorf("output = %q, want the tester roster with its description", out)
	}
	for _, want := range []string{"Managing your context is important", "crew delegate <role>", "Delegate early"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want delegation guidance %q", out, want)
		}
	}
}

// No active roles anywhere here should cost no context.
func TestRosterIsSilentWithNoActiveRoles(t *testing.T) {
	ctx := context.Background()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)

	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sess := &session.Session{
		ID: "s1", Harness: session.HarnessCodex, Status: session.StatusActive,
		Place:     session.Place{CWD: repo, Repo: &session.Repo{Name: "repo", Root: repo, MainRoot: repo}},
		StartedAt: time.Now(), LastSeen: time.Now(),
	}

	out, err := roomsFor(ctx, st, hook.EventStart, sess, sess.Key())
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if strings.Contains(out, "Managing your context") {
		t.Errorf("output = %q, want no roster with nothing activated", out)
	}
}

// An active role with no matching definition file still shows up by name:
// the roster is advisory context, not something the agent must be able to
// resolve to a file to see.
func TestRosterFallsBackToNameWithNoDefinition(t *testing.T) {
	ctx := context.Background()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)

	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.ActivateRole(ctx, &role.Activation{Room: repo, Role: "ghost", ActivatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	sess := &session.Session{
		ID: "s1", Harness: session.HarnessCodex, Status: session.StatusActive,
		Place:     session.Place{CWD: repo, Repo: &session.Repo{Name: "repo", Root: repo, MainRoot: repo}},
		StartedAt: time.Now(), LastSeen: time.Now(),
	}

	out, err := roomsFor(ctx, st, hook.EventStart, sess, sess.Key())
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if !strings.Contains(out, "ghost") {
		t.Errorf("output = %q, want the bare role name", out)
	}
}

// A worktree session belongs to two rooms, whose active roles share one roster.
func TestRosterCombinesRolesAcrossRooms(t *testing.T) {
	ctx := context.Background()
	home, worktree, mainRepo := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)

	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.ActivateRole(ctx, &role.Activation{Room: worktree, Role: "tester", ActivatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.ActivateRole(ctx, &role.Activation{Room: mainRepo, Role: "reviewer", ActivatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	sess := &session.Session{
		ID: "s1", Harness: session.HarnessCodex, Status: session.StatusActive,
		Place: session.Place{CWD: worktree, Repo: &session.Repo{
			Name: "repo", Root: worktree, MainRoot: mainRepo, IsWorktree: true,
		}},
		StartedAt: time.Now(), LastSeen: time.Now(),
	}

	out, err := roomsFor(ctx, st, hook.EventStart, sess, sess.Key())
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if !strings.Contains(out, "tester") || !strings.Contains(out, "reviewer") {
		t.Errorf("output = %q, want both rooms' roles in the roster", out)
	}
}
