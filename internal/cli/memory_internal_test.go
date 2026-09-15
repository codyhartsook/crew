package cli

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// seedActiveSession registers a live session at cwd's room, so ResolveAuthor
// can find it the way a running agent's own hook would have left it.
func seedActiveSession(t *testing.T, dbPath, cwd, id string) {
	t.Helper()
	ctx := context.Background()
	loc, err := detect.New().Detect(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := &session.Session{
		ID: id, Harness: session.HarnessCodex, Status: session.StatusActive,
		Place: session.Place{CWD: loc.CWD, Repo: loc.Repo}, StartedAt: time.Now(), LastSeen: time.Now(),
	}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
}

func runMemory(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	root := New()
	root.SetArgs(append([]string{"memory"}, args...))
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func TestMemoryRoundTripsForARole(t *testing.T) {
	cwd := t.TempDir()
	if out, err := runGit(t, cwd, "init", "-q", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "writer")
	t.Chdir(cwd)
	seedActiveSession(t, path, cwd, "writer")

	if _, err := runMemory(t, "--role", "tester", "found 3 failing tests"); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := runMemory(t, "--role", "tester")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "found 3 failing tests") {
		t.Errorf("output = %q, want the written entry", out)
	}
}

func TestMemoryIsolatesRoles(t *testing.T) {
	cwd := t.TempDir()
	if out, err := runGit(t, cwd, "init", "-q", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "writer")
	t.Chdir(cwd)
	seedActiveSession(t, path, cwd, "writer")

	if _, err := runMemory(t, "--role", "tester", "tester-secret"); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := runMemory(t, "--role", "reviewer")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(out, "tester-secret") {
		t.Errorf("reviewer's memory leaked the tester's entry: %q", out)
	}
}

// Without a role, memory falls back to the caller's own session, so it is
// still usable before any role or delegation exists.
func TestMemoryFallsBackToSessionScopeWithNoRole(t *testing.T) {
	cwd := t.TempDir()
	if out, err := runGit(t, cwd, "init", "-q", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "writer")
	t.Chdir(cwd)
	seedActiveSession(t, path, cwd, "writer")

	if _, err := runMemory(t, "a note to myself"); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := runMemory(t)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "a note to myself") {
		t.Errorf("output = %q, want the session-scoped entry", out)
	}
}

// Memory must never leak into the room briefing: it is a different table, not
// a visibility flag on entries.
func TestMemoryNeverAppearsInTheRoomBriefing(t *testing.T) {
	cwd := t.TempDir()
	if out, err := runGit(t, cwd, "init", "-q", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "writer")
	t.Chdir(cwd)
	seedActiveSession(t, path, cwd, "writer")

	if _, err := runMemory(t, "--role", "tester", "a secret finding"); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	if err := run(t, "post", "note", "a public note"); err != nil {
		t.Fatalf("post: %v", err)
	}

	var roomOut strings.Builder
	root := New()
	root.SetArgs([]string{"room", "--json"})
	root.SetOut(&roomOut)
	root.SetErr(&roomOut)
	if err := root.Execute(); err != nil {
		t.Fatalf("room --json: %v", err)
	}
	if strings.Contains(roomOut.String(), "a secret finding") {
		t.Errorf("room --json leaked role memory: %s", roomOut.String())
	}
	if !strings.Contains(roomOut.String(), "a public note") {
		t.Errorf("room --json = %q, want the public note", roomOut.String())
	}
}

// A delegated role's spawn sets CREW_AUTO_JOIN=0 specifically to stay outside
// the room, so it is never joined - but crew memory must still work for it:
// memory is a private, role-keyed channel, not gated by room membership.
func TestMemoryWorksForADelegatedRoleWithNoRoomMembership(t *testing.T) {
	cwd := t.TempDir()
	if out, err := runGit(t, cwd, "init", "-q", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "delegate")
	t.Setenv("CREW_AUTO_JOIN", "0")
	t.Chdir(cwd)
	seedActiveSession(t, path, cwd, "delegate")

	if _, err := runMemory(t, "--role", "tester", "found the failing test"); err != nil {
		t.Fatalf("write memory as an unjoined delegated role: %v", err)
	}
	out, err := runMemory(t, "--role", "tester")
	if err != nil {
		t.Fatalf("read memory as an unjoined delegated role: %v", err)
	}
	if !strings.Contains(out, "found the failing test") {
		t.Errorf("output = %q, want the entry the delegated role just wrote", out)
	}
}

// The same delegated role must still be denied the room itself: memory does
// not require membership, but posting does, and CREW_AUTO_JOIN=0 means it was
// never granted.
func TestPostStaysDeniedForADelegatedRoleWithNoRoomMembership(t *testing.T) {
	cwd := t.TempDir()
	if out, err := runGit(t, cwd, "init", "-q", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", dbPath)
	t.Setenv("CODEX_THREAD_ID", "delegate")
	t.Setenv("CREW_AUTO_JOIN", "0")
	t.Chdir(cwd)
	seedActiveSession(t, dbPath, cwd, "delegate")

	if err := run(t, "post", "note", "should not be allowed"); err == nil {
		t.Fatal("post succeeded for a delegated role with no room membership, want it denied")
	}
}

func runGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
