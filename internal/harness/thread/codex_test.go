package thread

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// holdLock keeps path's flock held for the life of the test, so the source
// under test sees it the way a live codex process would.
func holdLock(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
}

func writeRollout(t *testing.T, home, id, cwd, threadSource, originator string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "09", "14")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The line stamp is later than the thread's own, as codex writes them.
	line := `{"timestamp":"2026-09-14T19:25:15.888Z","type":"session_meta","payload":{"session_id":"` + id +
		`","timestamp":"2026-09-14T19:18:59.084Z","cwd":"` + cwd +
		`","thread_source":"` + threadSource + `","originator":"` + originator + `"}}` + "\n"
	path := filepath.Join(dir, "rollout-2026-09-14T12-18-59-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, lockDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func lockPath(home, id string) string {
	return filepath.Join(home, lockDir, id+".lock")
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCodexSourceOpen(t *testing.T) {
	home := newHome(t)
	src := CodexSource{Home: func() string { return home }}

	// A dotfile lock is never a thread, held or not.
	touch(t, filepath.Join(home, lockDir, ".coordination.lock"))

	// Held: a live thread with a rollout to read.
	heldID := "01a0819c-0000-7000-8000-000000000001"
	touch(t, lockPath(home, heldID))
	holdLock(t, lockPath(home, heldID))
	writeRollout(t, home, heldID, "/work/held", "user", "codex-tui")

	// Free: nobody holds this lock, so it is not an open thread.
	freeID := "01a0819c-0000-7000-8000-000000000002"
	touch(t, lockPath(home, freeID))

	// Held with no rollout on disk: still open, but nothing to read.
	orphanID := "01a0819c-0000-7000-8000-000000000003"
	touch(t, lockPath(home, orphanID))
	holdLock(t, lockPath(home, orphanID))

	// Two threads sharing a first UUID segment must resolve to their own
	// rollout, never each other's.
	sib1 := "01a0a15b-bce4-7142-a461-4de7cb510bd8"
	sib2 := "01a0a15b-bd5b-7971-a96c-dae8669f6ceb"
	touch(t, lockPath(home, sib1))
	holdLock(t, lockPath(home, sib1))
	writeRollout(t, home, sib1, "/work/sib1", "user", "codex-tui")
	touch(t, lockPath(home, sib2))
	holdLock(t, lockPath(home, sib2))
	writeRollout(t, home, sib2, "/work/sib2", "guardian_review", "codex-tui")

	open, err := src.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, ok := open[freeID]; ok {
		t.Error("a free lock should not be reported open")
	}
	if _, ok := open[".coordination"]; ok {
		t.Error("the dotfile lock should never be indexed")
	}

	held, ok := open[heldID]
	if !ok {
		t.Fatal("held thread missing from open set")
	}
	if held.CWD != "/work/held" || held.Source != "user" || held.Originator != "codex-tui" {
		t.Errorf("held thread = %+v", held)
	}
	if held.Path == "" || held.Active.IsZero() {
		t.Errorf("held thread should carry its rollout path and mtime: %+v", held)
	}

	orphan, ok := open[orphanID]
	if !ok {
		t.Fatal("held thread with no rollout should still be open")
	}
	if orphan.Path != "" || orphan.CWD != "" {
		t.Errorf("orphan thread should carry no rollout data: %+v", orphan)
	}

	got1, ok := open[sib1]
	if !ok {
		t.Fatal("sib1 missing")
	}
	got2, ok := open[sib2]
	if !ok {
		t.Fatal("sib2 missing")
	}
	if got1.CWD != "/work/sib1" || got1.Source != "user" {
		t.Errorf("sib1 resolved to the wrong rollout: %+v", got1)
	}
	if got2.CWD != "/work/sib2" || got2.Source != "guardian_review" {
		t.Errorf("sib2 resolved to the wrong rollout: %+v", got2)
	}
	if got1.Path == got2.Path {
		t.Errorf("siblings sharing a UUID prefix resolved to the same rollout: %s", got1.Path)
	}
}

func TestCodexSourceOpenMissingLockDirIsAnError(t *testing.T) {
	home := t.TempDir() // no thread-writer-locks subdirectory at all
	src := CodexSource{Home: func() string { return home }}
	if _, err := src.Open(); err == nil {
		t.Error("a missing lock directory should be an error, not an empty set")
	}
}

func TestProbeLockHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.lock")
	touch(t, path)
	if probeLockHeld(path) {
		t.Error("an unlocked file should not read as held")
	}
	holdLock(t, path)
	if !probeLockHeld(path) {
		t.Error("a locked file should read as held")
	}
}

func TestIndexRolloutsMatchesFullID(t *testing.T) {
	home := t.TempDir()
	sib1 := "01a0a15b-bce4-7142-a461-4de7cb510bd8"
	sib2 := "01a0a15b-bd5b-7971-a96c-dae8669f6ceb"
	writeRollout(t, home, sib1, "/a", "user", "codex-tui")
	writeRollout(t, home, sib2, "/b", "user", "codex-tui")

	index := indexRollouts(home)
	if len(index) != 2 {
		t.Fatalf("index = %v, want 2 entries", index)
	}
	if !strings.HasSuffix(index[sib1], sib1+".jsonl") || !strings.HasSuffix(index[sib2], sib2+".jsonl") {
		t.Errorf("index resolved to the wrong file: %v", index)
	}
}

// A stale mtime far in the past must still come back accurately, since
// last_seen refresh depends on it.
func TestReadRolloutReturnsMtime(t *testing.T) {
	home := t.TempDir()
	id := "01a0819c-0000-7000-8000-00000000000a"
	writeRollout(t, home, id, "/work", "user", "codex-tui")
	path := indexRollouts(home)[id]
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	meta, _, active, err := readRollout(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.CWD != "/work" {
		t.Errorf("meta = %+v", meta)
	}
	if active.Sub(old).Abs() > time.Second {
		t.Errorf("active = %v, want ~%v", active, old)
	}
}

// Started is when the thread began, not when its meta line was appended, so an
// adopted session cannot show a last_seen that precedes its own start.
func TestReadRolloutPrefersThreadStart(t *testing.T) {
	home := t.TempDir()
	id := "01a0819c-0000-7000-8000-00000000000b"
	writeRollout(t, home, id, "/work", "user", "codex-tui")
	_, started, _, err := readRollout(indexRollouts(home)[id])
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 14, 19, 18, 59, 84_000_000, time.UTC)
	if !started.Equal(want) {
		t.Errorf("started = %v, want %v", started, want)
	}
}
