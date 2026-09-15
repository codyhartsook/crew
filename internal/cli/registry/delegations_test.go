package registry

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func newTestDelegation(t *testing.T, st *sqlitestore.Store, id string) {
	t.Helper()
	now := time.Now().UTC()
	d := &delegation.Delegation{
		ID: id, Room: "/repo", Dir: "/repo", Role: "tester", Harness: "codex",
		Requester: "codex:a", Prompt: "run the tests", Status: delegation.StatusPending,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateDelegation(context.Background(), d); err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	if _, err := st.StartDelegation(context.Background(), id); err != nil {
		t.Fatalf("StartDelegation: %v", err)
	}
}

// backdate simulates a delegation last touched long ago, as a claim from a
// process that has since crashed or restarted would be.
func backdate(t *testing.T, path, id string, age time.Duration) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer raw.Close()
	stale := time.Now().UTC().Add(-age).Format(time.RFC3339Nano)
	if _, err := raw.Exec(`UPDATE delegations SET updated_at = ? WHERE id = ?`, stale, id); err != nil {
		t.Fatalf("backdate: %v", err)
	}
}

func testCoordinator(st *sqlitestore.Store) *delegationCoordinator {
	return newDelegationCoordinator(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestReapStaleFailsAnOrphanedRunningDelegation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	newTestDelegation(t, st, "orphan")
	backdate(t, path, "orphan", staleRunningTimeout+time.Minute)

	testCoordinator(st).reapStale(ctx)

	got, err := st.GetDelegation(ctx, "orphan")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != delegation.StatusFailed {
		t.Errorf("Status = %q, want failed", got.Status)
	}
}

// A spawn can legitimately run for a while; reapStale must not fail one
// simply for being in progress.
func TestReapStaleLeavesARecentRunningDelegationAlone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	newTestDelegation(t, st, "in-flight")

	testCoordinator(st).reapStale(ctx)

	got, err := st.GetDelegation(ctx, "in-flight")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != delegation.StatusRunning {
		t.Errorf("Status = %q, want it left running", got.Status)
	}
}

// Even a stale-by-age row must not be touched while this process's own
// in-memory claim says it is the one executing it.
func TestReapStaleLeavesThisProcessesOwnClaimAlone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	newTestDelegation(t, st, "mine")
	backdate(t, path, "mine", staleRunningTimeout+time.Minute)

	c := testCoordinator(st)
	c.claim("mine")
	c.reapStale(ctx)

	got, err := st.GetDelegation(ctx, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != delegation.StatusRunning {
		t.Errorf("Status = %q, want it left running: this process still claims it", got.Status)
	}
}
