package registry

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func openLivenessTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// codexHomeFixture points the real codex harness spec's thread source at a
// temp CODEX_HOME, so the coordinator under test runs its real adoption path.
func codexHomeFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "thread-writer-locks"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	return home
}

// holdCodexLock creates a thread-writer lock file and keeps it flocked for the
// life of the test, the way a live codex process would.
func holdCodexLock(t *testing.T, home, id string) {
	t.Helper()
	path := filepath.Join(home, "thread-writer-locks", id+".lock")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
}

// rolloutStart is the thread start every fixture rollout claims.
var rolloutStart = time.Date(2026, 9, 14, 19, 18, 59, 0, time.UTC)

func writeCodexRollout(t *testing.T, home, id, cwd, threadSource, originator string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "09", "14")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"timestamp":"2026-09-14T19:25:15.888Z","type":"session_meta","payload":{"session_id":"` + id +
		`","timestamp":"` + rolloutStart.Format(time.RFC3339Nano) + `","cwd":"` + cwd +
		`","thread_source":"` + threadSource + `","originator":"` + originator + `"}}` + "\n"
	path := filepath.Join(dir, "rollout-2026-09-14T12-18-59-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// A guardian_review thread must never be adopted: only thread_source "user" is
// a conversation a person is driving. Without this filter one terminal opening
// a guardian review would register as a second session.
func TestLivenessAdoptsOnlyUserThreads(t *testing.T) {
	home := codexHomeFixture(t)
	cwd := t.TempDir()
	now := time.Now()

	userID := "01a0a609-0000-7000-8000-000000000001"
	guardianID := "01a0a609-0000-7000-8000-000000000002"
	holdCodexLock(t, home, userID)
	holdCodexLock(t, home, guardianID)
	writeCodexRollout(t, home, userID, cwd, "user", "codex-tui", now)
	writeCodexRollout(t, home, guardianID, cwd, "guardian_review", "codex-tui", now)

	st := openLivenessTestStore(t)
	c := newLivenessCoordinator(st, slog.New(slog.DiscardHandler))
	c.reconcileThreads(context.Background())

	got, err := st.Get(context.Background(), "codex:"+userID)
	if err != nil {
		t.Fatalf("adopted session missing: %v", err)
	}
	if got.PID != 0 {
		t.Errorf("adopted session PID = %d, want 0: liveness for it comes from the lock, not a pid", got.PID)
	}
	if got.Meta["originator"] != "codex-tui" {
		t.Errorf("adopted session originator = %q, want codex-tui", got.Meta["originator"])
	}
	if got.Alias == "" {
		t.Error("adopted session has no alias")
	}

	if _, err := st.Get(context.Background(), "codex:"+guardianID); err == nil {
		t.Error("a guardian_review thread should never be adopted")
	}
}

// An adopted session starts when its thread did, not when the sweep noticed
// it, so last_seen can never precede its own start.
func TestLivenessAdoptsWithTheThreadsOwnStart(t *testing.T) {
	home := codexHomeFixture(t)
	cwd := t.TempDir()
	id := "01a0a609-0000-7000-8000-000000000009"
	holdCodexLock(t, home, id)
	writeCodexRollout(t, home, id, cwd, "user", "codex-tui", rolloutStart.Add(time.Hour))

	st := openLivenessTestStore(t)
	newLivenessCoordinator(st, slog.New(slog.DiscardHandler)).reconcileThreads(context.Background())

	got, err := st.Get(context.Background(), "codex:"+id)
	if err != nil {
		t.Fatalf("adopted session missing: %v", err)
	}
	if !got.StartedAt.Equal(rolloutStart) {
		t.Errorf("StartedAt = %v, want the thread's own start %v", got.StartedAt, rolloutStart)
	}
	if got.LastSeen.Before(got.StartedAt) {
		t.Errorf("LastSeen %v precedes StartedAt %v", got.LastSeen, got.StartedAt)
	}
}

// Adoption must not re-register a thread the registry already has active.
func TestLivenessDoesNotReadoptARegisteredThread(t *testing.T) {
	home := codexHomeFixture(t)
	cwd := t.TempDir()
	id := "01a0a609-0000-7000-8000-000000000003"
	holdCodexLock(t, home, id)
	writeCodexRollout(t, home, id, cwd, "user", "codex-tui", time.Now())

	st := openLivenessTestStore(t)
	ctx := context.Background()
	existing := &session.Session{
		ID: id, Harness: session.HarnessCodex, Status: session.StatusActive,
		PID: 4242, LastSeen: time.Now(),
	}
	if err := st.Upsert(ctx, existing); err != nil {
		t.Fatalf("seed: %v", err)
	}

	c := newLivenessCoordinator(st, slog.New(slog.DiscardHandler))
	c.reconcileThreads(ctx)

	got, err := st.Get(ctx, "codex:"+id)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 4242 {
		t.Errorf("re-adoption overwrote the existing session: PID = %d, want 4242 unchanged", got.PID)
	}
}

// last_seen takes the later of the stored value and the conversation file's
// mtime: a stale one is pulled forward, a fresher one is left alone. This
// covers both a codex session (rollout mtime via the thread source) and a
// claude session (transcript mtime, which has no thread source at all).
func TestLivenessRefreshesLastSeenToTheLaterOfStoredAndMtime(t *testing.T) {
	home := codexHomeFixture(t)
	cwd := t.TempDir()
	now := time.Now()

	codexID := "01a0a609-0000-7000-8000-000000000004"
	holdCodexLock(t, home, codexID)
	rolloutMtime := now.Add(-time.Hour)
	writeCodexRollout(t, home, codexID, cwd, "user", "codex-tui", rolloutMtime)

	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriptMtime := now.Add(-30 * time.Minute)
	if err := os.Chtimes(transcript, transcriptMtime, transcriptMtime); err != nil {
		t.Fatal(err)
	}

	st := openLivenessTestStore(t)
	ctx := context.Background()

	staleCodex := &session.Session{
		ID: codexID, Harness: session.HarnessCodex, Status: session.StatusActive,
		LastSeen: now.Add(-6 * time.Hour), // e.g. a prompt-hook timestamp that lagged
	}
	freshClaude := &session.Session{
		ID: "c1", Harness: session.HarnessClaude, Status: session.StatusActive,
		LastSeen: now.Add(-time.Minute), // newer than the transcript
		Meta:     map[string]string{"transcript_path": transcript},
	}
	staleClaude := &session.Session{
		ID: "c2", Harness: session.HarnessClaude, Status: session.StatusActive,
		LastSeen: now.Add(-6 * time.Hour),
		Meta:     map[string]string{"transcript_path": transcript},
	}
	for _, s := range []*session.Session{staleCodex, freshClaude, staleClaude} {
		if err := st.Upsert(ctx, s); err != nil {
			t.Fatalf("seed %s: %v", s.Key(), err)
		}
	}

	c := newLivenessCoordinator(st, slog.New(slog.DiscardHandler))
	c.reconcileThreads(ctx)

	gotCodex, err := st.Get(ctx, staleCodex.Key())
	if err != nil {
		t.Fatal(err)
	}
	if gotCodex.LastSeen.Sub(rolloutMtime).Abs() > time.Second {
		t.Errorf("codex last_seen = %v, want ~%v (the rollout mtime)", gotCodex.LastSeen, rolloutMtime)
	}

	gotFresh, err := st.Get(ctx, freshClaude.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !gotFresh.LastSeen.After(transcriptMtime) {
		t.Errorf("a fresher stored last_seen must not be pulled backward to the mtime: %v", gotFresh.LastSeen)
	}

	gotStale, err := st.Get(ctx, staleClaude.Key())
	if err != nil {
		t.Fatal(err)
	}
	if gotStale.LastSeen.Sub(transcriptMtime).Abs() > time.Second {
		t.Errorf("claude last_seen = %v, want ~%v (the transcript mtime)", gotStale.LastSeen, transcriptMtime)
	}
}
