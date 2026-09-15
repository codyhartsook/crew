package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/routing"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// RoutingFactory builds an empty routing store for one subtest.
type RoutingFactory func(t *testing.T) store.RoutingStore

// RunRouting executes the routing log conformance suite against an
// implementation.
func RunRouting(t *testing.T, newStore RoutingFactory) {
	t.Helper()
	tests := map[string]func(*testing.T, RoutingFactory){
		"LogValidates":           testRoutingLogValidates,
		"LogAssignsIDs":          testRoutingLogAssignsIDs,
		"LogScopedToRoomInOrder": testRoutingLogScoped,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

func testRoutingLogValidates(t *testing.T, newStore RoutingFactory) {
	s := newStore(t)
	cases := []*routing.Decision{
		{Session: "codex:a", Roles: []string{"tester"}},
		{Room: "/repo", Session: "codex:a"},
	}
	for _, d := range cases {
		if err := s.LogRouting(context.Background(), d); err == nil {
			t.Errorf("LogRouting(%+v) = nil, want an error", d)
		}
	}
}

func testRoutingLogAssignsIDs(t *testing.T, newStore RoutingFactory) {
	s := newStore(t)
	d := &routing.Decision{Room: "/repo", Session: "codex:a", Roles: []string{"tester"}, CreatedAt: time.Now()}
	if err := s.LogRouting(context.Background(), d); err != nil {
		t.Fatalf("LogRouting: %v", err)
	}
	if d.ID == 0 {
		t.Error("LogRouting did not assign an id")
	}
}

func testRoutingLogScoped(t *testing.T, newStore RoutingFactory) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	if err := s.LogRouting(ctx, &routing.Decision{Room: "/repo-a", Session: "codex:a", Roles: []string{"tester"}, CreatedAt: now}); err != nil {
		t.Fatalf("LogRouting: %v", err)
	}
	if err := s.LogRouting(ctx, &routing.Decision{Room: "/repo-a", Session: "codex:a", Roles: []string{"reviewer"}, CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("LogRouting: %v", err)
	}
	if err := s.LogRouting(ctx, &routing.Decision{Room: "/repo-b", Session: "codex:b", Roles: []string{"tester"}, CreatedAt: now}); err != nil {
		t.Fatalf("LogRouting: %v", err)
	}

	log, err := s.RoutingLog(ctx, "/repo-a")
	if err != nil {
		t.Fatalf("RoutingLog: %v", err)
	}
	if len(log) != 2 || log[0].Roles[0] != "tester" || log[1].Roles[0] != "reviewer" {
		t.Errorf("RoutingLog(/repo-a) = %+v, want [tester, reviewer] in order", log)
	}
}
