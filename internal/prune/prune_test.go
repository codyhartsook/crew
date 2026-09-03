package prune

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/proc"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

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

func sess(id string, harness session.Harness, pid int, host string, lastSeen time.Time) *session.Session {
	return &session.Session{
		ID: id, Harness: harness, Status: session.StatusActive,
		PID: pid, Host: host, LastSeen: lastSeen,
	}
}

func TestDeadAmong(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	table := proc.Table{
		100: {PID: 100, PPID: 1, Command: "codex"},
		200: {PID: 200, PPID: 1, Command: "claude"},
		300: {PID: 300, PPID: 1, Command: "zsh"},
	}

	cases := []struct {
		name string
		in   *session.Session
		dead bool
	}{
		{"live process", sess("a", session.HarnessCodex, 100, "here", old), false},
		{"process gone", sess("b", session.HarnessCodex, 999, "here", old), true},
		// A pid can be recycled by something unrelated; the command has to match.
		{"pid reused by another program", sess("c", session.HarnessCodex, 300, "here", old), true},
		// Judging a pid on a machine we cannot see would reap live sessions.
		{"another host", sess("d", session.HarnessCodex, 999, "elsewhere", old), false},
		{"no pid recorded", sess("e", session.HarnessCodex, 0, "here", old), false},
		// A pid recorded moments ago that already looks wrong is far more
		// likely misread than dead.
		{"within the grace period", sess("f", session.HarnessCodex, 999, "here", now.Add(-time.Second)), false},
		{"wrong harness for the pid", sess("g", session.HarnessClaude, 100, "here", old), true},
		{"right harness for the pid", sess("h", session.HarnessClaude, 200, "here", old), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deadAmong([]*session.Session{tc.in}, table, "here", now)
			if (len(got) == 1) != tc.dead {
				t.Errorf("deadAmong = %v, want dead=%v", got, tc.dead)
			}
		})
	}
}

// An empty process table means inspection failed, not that everything died.
func TestDeadAmongEmptyTableReapsNothingUsable(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	in := []*session.Session{sess("a", session.HarnessCodex, 100, "here", now.Add(-time.Hour))}
	// deadAmong itself would call everything dead, which is why findDead
	// refuses to run on an empty table at all.
	if got := deadAmong(in, proc.Table{}, "here", now); len(got) != 1 {
		t.Fatalf("deadAmong on an empty table = %v, want it to report dead", got)
	}
}

// An unreadable or blocked process table must report that it cannot tell,
// rather than formatting a nil error into the message.
func TestDeadReportsAnEmptyProcessTable(t *testing.T) {
	st := seeded(t, sess("a", session.HarnessClaude, 1, "h", time.Now().UTC().Add(-time.Hour)))

	snapshot = func() (proc.Table, error) { return proc.Table{}, nil }
	defer func() { snapshot = proc.Snapshot }()

	_, err := Dead(context.Background(), st)
	if err == nil || strings.Contains(err.Error(), "%!w") {
		t.Fatalf("Dead with an empty table = %v, want a plain explanation", err)
	}
}
