package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// RoleFactory builds an empty role store for one subtest.
type RoleFactory func(t *testing.T) store.RoleStore

// RunRoles executes the role activation conformance suite against an
// implementation.
func RunRoles(t *testing.T, newStore RoleFactory) {
	t.Helper()
	tests := map[string]func(*testing.T, RoleFactory){
		"ActivateValidates":             testActivateValidates,
		"ActivateTwiceKeepsFirstTime":   testActivateIdempotent,
		"DeactivateRemovesIt":           testDeactivate,
		"DeactivateUnknownIsNotAnError": testDeactivateUnknown,
		"ActiveRolesScopedToRoom":       testActiveRolesScoped,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

func activation(room, roleName string, at time.Time) *role.Activation {
	return &role.Activation{Room: room, Role: roleName, ActivatedAt: at}
}

func testActivateValidates(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	cases := []*role.Activation{
		{Role: "tester"},
		{Room: "/repo"},
	}
	for _, a := range cases {
		if err := s.ActivateRole(context.Background(), a); err == nil {
			t.Errorf("ActivateRole(%+v) = nil, want an error", a)
		}
	}
}

func testActivateIdempotent(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	ctx := context.Background()
	first := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	later := first.Add(time.Hour)

	if err := s.ActivateRole(ctx, activation("/repo", "tester", first)); err != nil {
		t.Fatalf("ActivateRole: %v", err)
	}
	if err := s.ActivateRole(ctx, activation("/repo", "tester", later)); err != nil {
		t.Fatalf("ActivateRole (repeat): %v", err)
	}
	active, err := s.ActiveRoles(ctx, "/repo")
	if err != nil {
		t.Fatalf("ActiveRoles: %v", err)
	}
	if len(active) != 1 || !active[0].ActivatedAt.Equal(first) {
		t.Errorf("ActiveRoles = %+v, want one entry activated at %v", active, first)
	}
}

func testDeactivate(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.ActivateRole(ctx, activation("/repo", "tester", time.Now())); err != nil {
		t.Fatalf("ActivateRole: %v", err)
	}
	if err := s.DeactivateRole(ctx, "/repo", "tester"); err != nil {
		t.Fatalf("DeactivateRole: %v", err)
	}
	active, err := s.ActiveRoles(ctx, "/repo")
	if err != nil {
		t.Fatalf("ActiveRoles: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("ActiveRoles after deactivate = %+v, want none", active)
	}
}

func testDeactivateUnknown(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	if err := s.DeactivateRole(context.Background(), "/repo", "never-activated"); err != nil {
		t.Errorf("DeactivateRole(unknown) = %v, want nil", err)
	}
}

func testActiveRolesScoped(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.ActivateRole(ctx, activation("/repo-a", "tester", time.Now())); err != nil {
		t.Fatalf("ActivateRole: %v", err)
	}
	if err := s.ActivateRole(ctx, activation("/repo-b", "reviewer", time.Now())); err != nil {
		t.Fatalf("ActivateRole: %v", err)
	}
	active, err := s.ActiveRoles(ctx, "/repo-a")
	if err != nil {
		t.Fatalf("ActiveRoles: %v", err)
	}
	if len(active) != 1 || active[0].Role != "tester" {
		t.Errorf("ActiveRoles(/repo-a) = %+v, want only tester", active)
	}
}
