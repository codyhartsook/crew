package roomctx

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

const testRoom = "/src/widget"

func seeded(t *testing.T, sessions ...*session.Session) store.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, s := range sessions {
		if err := st.Upsert(context.Background(), s); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	return st
}

func agent(harness session.Harness, id string, pid int) *session.Session {
	now := time.Now().UTC()
	return &session.Session{
		ID: id, Harness: harness, Status: session.StatusActive, PID: pid,
		Place: session.Place{
			CWD:  testRoom,
			Repo: &session.Repo{Name: "widget", Root: testRoom, MainRoot: testRoom},
		},
		StartedAt: now, LastSeen: now,
	}
}

// clearEnv keeps a developer's own harness environment out of the test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, spec := range harness.Specs() {
		for _, env := range spec.SessionEnv {
			t.Setenv(env, "")
		}
	}
}

// The environment is exact, and is the only source that survives a harness
// running its commands inside a sandbox where process inspection is blocked.
func TestResolveAuthorPrefersEnvironment(t *testing.T) {
	clearEnv(t)
	st := seeded(t, agent(session.HarnessCodex, "aaa", 0), agent(session.HarnessClaude, "bbb", 0))
	t.Setenv("CODEX_THREAD_ID", "aaa")

	got, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("resolveAuthor: %v", err)
	}
	if got != "codex:aaa" {
		t.Errorf("author = %q, want %q", got, "codex:aaa")
	}
}

func TestResolveAuthorClaudeEnv(t *testing.T) {
	clearEnv(t)
	st := seeded(t, agent(session.HarnessClaude, "bbb", 0), agent(session.HarnessCodex, "aaa", 0))
	t.Setenv("CLAUDE_CODE_SESSION_ID", "bbb")

	got, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("resolveAuthor: %v", err)
	}
	if got != "claude:bbb" {
		t.Errorf("author = %q, want %q", got, "claude:bbb")
	}
}

// A session id the registry has never seen must not be trusted blindly; a
// stale variable would otherwise attribute entries to a session that is gone.
func TestResolveAuthorIgnoresUnknownEnv(t *testing.T) {
	clearEnv(t)
	st := seeded(t, agent(session.HarnessCodex, "aaa", 0))
	t.Setenv("CLAUDE_CODE_SESSION_ID", "not-registered")

	got, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("resolveAuthor: %v", err)
	}
	if got != "codex:aaa" {
		t.Errorf("author = %q, want the sole session here", got)
	}
}

func TestResolveAuthorSoleSession(t *testing.T) {
	clearEnv(t)
	st := seeded(t, agent(session.HarnessCodex, "aaa", 0))

	got, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("resolveAuthor: %v", err)
	}
	if got != "codex:aaa" {
		t.Errorf("author = %q, want %q", got, "codex:aaa")
	}
}

// Failing is fine; failing without telling the caller what to pass is not.
func TestResolveAuthorAmbiguousNamesCandidates(t *testing.T) {
	clearEnv(t)
	st := seeded(t,
		agent(session.HarnessCodex, "aaa", 0),
		agent(session.HarnessClaude, "bbb", 0),
		agent(session.HarnessClaude, "ccc", 0),
	)

	_, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err == nil {
		t.Fatal("resolveAuthor succeeded with three indistinguishable agents")
	}
	for _, want := range []string{"--as", "codex:aaa", "claude:bbb", "claude:ccc"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q: %v", want, err)
		}
	}
}

func TestResolveAuthorNoSessions(t *testing.T) {
	clearEnv(t)
	st := seeded(t)

	if _, err := ResolveAuthor(context.Background(), st, []string{testRoom}); err == nil {
		t.Fatal("resolveAuthor succeeded with no active sessions")
	}
}

// A harness launched from inside another inherits its variables, so the first
// identity in the environment is not necessarily the caller's.
func TestResolveAuthorPicksTheRegisteredEnvIdentity(t *testing.T) {
	clearEnv(t)
	st := seeded(t, agent(session.HarnessCodex, "aaa", 0))
	// Inherited from an outer Claude session that was never registered.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "outer-claude")
	t.Setenv("CODEX_THREAD_ID", "aaa")

	got, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("resolveAuthor: %v", err)
	}
	if got != "codex:aaa" {
		t.Errorf("author = %q, want the registered codex session, not the inherited one", got)
	}
}

// When both identities are registered, the one in this room wins.
func TestResolveAuthorPrefersTheIdentityInThisRoom(t *testing.T) {
	clearEnv(t)
	outer := agent(session.HarnessClaude, "outer", 0)
	outer.Repo = &session.Repo{Name: "other", Root: "/src/other", MainRoot: "/src/other"}
	st := seeded(t, outer, agent(session.HarnessCodex, "aaa", 0))
	t.Setenv("CLAUDE_CODE_SESSION_ID", "outer")
	t.Setenv("CODEX_THREAD_ID", "aaa")

	got, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("resolveAuthor: %v", err)
	}
	if got != "codex:aaa" {
		t.Errorf("author = %q, want the session in this room", got)
	}
}

// A harness that spawns hooks through a shared shell records the same pid for
// several sessions. Matching on it would file entries under the wrong agent.
func TestResolveAuthorRejectsSharedPID(t *testing.T) {
	clearEnv(t)
	shared := os.Getpid()
	mine := agent(session.HarnessCodex, "mine", shared)
	other := agent(session.HarnessClaude, "other", shared)
	other.Repo = &session.Repo{Name: "other", Root: "/src/other", MainRoot: "/src/other"}
	st := seeded(t, mine, other)

	// Only "mine" is in this room, so the room must decide it, not the pid.
	got, err := ResolveAuthor(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("resolveAuthor: %v", err)
	}
	if got != "codex:mine" {
		t.Errorf("author = %q, want the session in this room, not whichever shares the pid", got)
	}
}

// A pid shared by two sessions that are both in this room is unresolvable, and
// must fail rather than pick one.
func TestResolveAuthorSharedPIDInSameRoomFails(t *testing.T) {
	clearEnv(t)
	shared := os.Getpid()
	st := seeded(t,
		agent(session.HarnessCodex, "a", shared),
		agent(session.HarnessClaude, "b", shared),
	)

	if _, err := ResolveAuthor(context.Background(), st, []string{testRoom}); err == nil {
		t.Fatal("resolveAuthor picked one of two sessions sharing a pid")
	}
}

// An ancestor pid belonging to a session working somewhere else is a
// coincidence, not the caller.
func TestResolveAuthorIgnoresAncestorOutsideRoom(t *testing.T) {
	clearEnv(t)
	elsewhere := agent(session.HarnessClaude, "elsewhere", os.Getpid())
	elsewhere.Repo = &session.Repo{Name: "other", Root: "/src/other", MainRoot: "/src/other"}
	st := seeded(t, elsewhere)

	if _, err := ResolveAuthor(context.Background(), st, []string{testRoom}); err == nil {
		t.Fatal("resolveAuthor claimed a session that is working in another room")
	}
}

func TestResolveAuthorRejectsEnvironmentOutsideRoom(t *testing.T) {
	clearEnv(t)
	elsewhere := agent(session.HarnessCodex, "elsewhere", 0)
	elsewhere.Repo = &session.Repo{Name: "other", Root: "/src/other", MainRoot: "/src/other"}
	st := seeded(t, elsewhere)
	t.Setenv("CODEX_THREAD_ID", elsewhere.ID)

	if _, err := ResolveAuthor(context.Background(), st, []string{testRoom}); err == nil || !strings.Contains(err.Error(), "not in this room") {
		t.Fatalf("ResolveAuthor outside room error = %v, want room error", err)
	}
}

func TestResolveAgentByAlias(t *testing.T) {
	want := agent(session.HarnessClaude, "target", 0)
	st := seeded(t, want)
	got, err := ResolveAgent(context.Background(), st, []string{testRoom}, want.Alias)
	if err != nil {
		t.Fatalf("resolveAgent: %v", err)
	}
	if got != want.Key() {
		t.Errorf("recipient = %q, want %q", got, want.Key())
	}
}

func TestOwnAlias(t *testing.T) {
	clearEnv(t)
	want := agent(session.HarnessCodex, "mine", 0)
	st := seeded(t, want)
	t.Setenv("CODEX_THREAD_ID", want.ID)
	got, err := OwnAlias(context.Background(), st, []string{testRoom})
	if err != nil {
		t.Fatalf("ownAlias: %v", err)
	}
	if got != want.Alias {
		t.Errorf("name = %q, want %q", got, want.Alias)
	}
}
