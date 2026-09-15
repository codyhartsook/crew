package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// DelegationFactory builds an empty delegation store for one subtest.
type DelegationFactory func(t *testing.T) store.DelegationStore

// RunDelegations executes the delegation conformance suite against an
// implementation.
func RunDelegations(t *testing.T, newStore DelegationFactory) {
	t.Helper()
	tests := map[string]func(*testing.T, DelegationFactory){
		"CreateValidates":                     testDelegationCreateValidates,
		"StartClaimsAPendingDelegation":       testDelegationStart,
		"StartFailsOnceAlreadyClaimed":        testDelegationStartTwice,
		"CompleteRequiresRunning":             testDelegationComplete,
		"FailRequiresRunning":                 testDelegationFail,
		"ListFiltersByRequesterAndUnnotified": testDelegationListFilters,
		"MarkNotified":                        testDelegationMarkNotified,
		"DirRoundTripsSeparatelyFromRoom":     testDelegationDirRoundTrips,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

func newDelegation(id, room, role, requester string) *delegation.Delegation {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	return &delegation.Delegation{
		// Dir deliberately differs from Room: a pooled worktree's room key is
		// not a filesystem path, so a test that only ever sets them equal
		// would not catch the two columns being swapped.
		ID: id, Room: room, Dir: room + "#lease", Role: role, Harness: "codex", Requester: requester,
		Prompt: "run the tests", Status: delegation.StatusPending,
		CreatedAt: now, UpdatedAt: now,
	}
}

func testDelegationCreateValidates(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	cases := []*delegation.Delegation{
		{Room: "/repo", Role: "tester"},
		{ID: "d1", Role: "tester"},
		{ID: "d1", Room: "/repo"},
	}
	for _, d := range cases {
		if err := s.CreateDelegation(context.Background(), d); err == nil {
			t.Errorf("CreateDelegation(%+v) = nil, want an error", d)
		}
	}
}

func testDelegationStart(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	ctx := context.Background()
	d := newDelegation("d1", "/repo", "tester", "codex:launcher")
	if err := s.CreateDelegation(ctx, d); err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	started, err := s.StartDelegation(ctx, "d1")
	if err != nil || !started {
		t.Fatalf("StartDelegation() = %v, %v, want true, nil", started, err)
	}
	got, err := s.GetDelegation(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDelegation: %v", err)
	}
	if got.Status != delegation.StatusRunning {
		t.Errorf("Status = %q, want running", got.Status)
	}
}

func testDelegationStartTwice(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	ctx := context.Background()
	d := newDelegation("d1", "/repo", "tester", "codex:launcher")
	if err := s.CreateDelegation(ctx, d); err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	if _, err := s.StartDelegation(ctx, "d1"); err != nil {
		t.Fatalf("StartDelegation: %v", err)
	}
	started, err := s.StartDelegation(ctx, "d1")
	if err != nil || started {
		t.Errorf("second StartDelegation() = %v, %v, want false, nil", started, err)
	}
}

func testDelegationComplete(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	ctx := context.Background()
	d := newDelegation("d1", "/repo", "tester", "codex:launcher")
	if err := s.CreateDelegation(ctx, d); err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}

	// Completing a delegation that never started must not fabricate a result.
	if err := s.CompleteDelegation(ctx, "d1", "done early"); err != nil {
		t.Fatalf("CompleteDelegation: %v", err)
	}
	if got, err := s.GetDelegation(ctx, "d1"); err != nil || got.Status != delegation.StatusPending {
		t.Fatalf("GetDelegation = %+v, %v, want still pending", got, err)
	}

	if _, err := s.StartDelegation(ctx, "d1"); err != nil {
		t.Fatalf("StartDelegation: %v", err)
	}
	if err := s.CompleteDelegation(ctx, "d1", `{"ok":true}`); err != nil {
		t.Fatalf("CompleteDelegation: %v", err)
	}
	got, err := s.GetDelegation(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDelegation: %v", err)
	}
	if got.Status != delegation.StatusDone || got.Result != `{"ok":true}` {
		t.Errorf("GetDelegation = %+v, want done with the result", got)
	}
}

func testDelegationFail(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	ctx := context.Background()
	d := newDelegation("d1", "/repo", "tester", "codex:launcher")
	if err := s.CreateDelegation(ctx, d); err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	if _, err := s.StartDelegation(ctx, "d1"); err != nil {
		t.Fatalf("StartDelegation: %v", err)
	}
	if err := s.FailDelegation(ctx, "d1", "boom"); err != nil {
		t.Fatalf("FailDelegation: %v", err)
	}
	got, err := s.GetDelegation(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDelegation: %v", err)
	}
	if got.Status != delegation.StatusFailed || got.Error != "boom" {
		t.Errorf("GetDelegation = %+v, want failed with the error", got)
	}
}

func testDelegationListFilters(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	ctx := context.Background()
	a := newDelegation("d1", "/repo", "tester", "codex:a")
	b := newDelegation("d2", "/repo", "tester", "codex:b")
	for _, d := range []*delegation.Delegation{a, b} {
		if err := s.CreateDelegation(ctx, d); err != nil {
			t.Fatalf("CreateDelegation: %v", err)
		}
	}

	got, err := s.ListDelegations(ctx, delegation.Filter{Requester: "codex:a"})
	if err != nil {
		t.Fatalf("ListDelegations: %v", err)
	}
	if len(got) != 1 || got[0].ID != "d1" {
		t.Errorf("ListDelegations(requester=codex:a) = %+v, want only d1", got)
	}

	if err := s.MarkNotified(ctx, "d1"); err != nil {
		t.Fatalf("MarkNotified: %v", err)
	}
	unnotified, err := s.ListDelegations(ctx, delegation.Filter{Unnotified: true})
	if err != nil {
		t.Fatalf("ListDelegations: %v", err)
	}
	if len(unnotified) != 1 || unnotified[0].ID != "d2" {
		t.Errorf("ListDelegations(unnotified) = %+v, want only d2", unnotified)
	}
}

// Dir is a distinct filesystem path from Room for a pooled worktree, so it
// must survive a round trip as its own column, not be confused with Room.
func testDelegationDirRoundTrips(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	ctx := context.Background()
	d := newDelegation("d1", "/repo#lease-1", "tester", "codex:a")
	if err := s.CreateDelegation(ctx, d); err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	got, err := s.GetDelegation(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDelegation: %v", err)
	}
	if got.Dir != d.Dir || got.Dir == got.Room {
		t.Errorf("GetDelegation().Dir = %q, want %q (distinct from Room %q)", got.Dir, d.Dir, got.Room)
	}
}

func testDelegationMarkNotified(t *testing.T, newStore DelegationFactory) {
	s := newStore(t)
	ctx := context.Background()
	d := newDelegation("d1", "/repo", "tester", "codex:a")
	if err := s.CreateDelegation(ctx, d); err != nil {
		t.Fatalf("CreateDelegation: %v", err)
	}
	if err := s.MarkNotified(ctx, "d1"); err != nil {
		t.Fatalf("MarkNotified: %v", err)
	}
	got, err := s.GetDelegation(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDelegation: %v", err)
	}
	if !got.Notified {
		t.Error("Notified = false, want true")
	}
}
