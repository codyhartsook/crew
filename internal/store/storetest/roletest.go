package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// RoleBackend holds sessions and roles, since "active only" joins them.
type RoleBackend interface {
	store.Store
	store.RoleStore
}

// RoleFactory builds an empty role store for one subtest.
type RoleFactory func(t *testing.T) RoleBackend

// RunRoles executes the role conformance suite against an implementation.
func RunRoles(t *testing.T, newStore RoleFactory) {
	t.Helper()
	tests := map[string]func(*testing.T, RoleFactory){
		"AssignReplaces":  testAssignReplaces,
		"AssignValidates": testAssignValidates,
		"Drop":            testDrop,
		"ActiveOnly":      testRolesActiveOnly,
		"RoomFilter":      testRolesRoomFilter,
		"SkillDirs":       testSkillDirs,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

func role(key, roomKey, name string) *store.Role {
	return &store.Role{
		SessionKey: key, Room: roomKey, Name: name,
		Description: "Runs the suite. Send finished changes.",
		AssignedAt:  time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
}

func mustAssign(t *testing.T, s store.RoleStore, r *store.Role) {
	t.Helper()
	if err := s.Assign(context.Background(), r); err != nil {
		t.Fatalf("Assign: %v", err)
	}
}

func roleNames(t *testing.T, s store.RoleStore, f store.RoleFilter) []string {
	t.Helper()
	got, err := s.Roles(context.Background(), f)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	var out []string
	for _, r := range got {
		out = append(out, r.SessionKey+"="+r.Name)
	}
	return out
}

func assertRoles(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
}

func testAssignReplaces(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	mustAssign(t, s, role(agentA, worktreeRoom, "tester"))
	replaced := role(agentA, worktreeRoom, "reviewer")
	replaced.Description = "Reviews diffs."
	mustAssign(t, s, replaced)

	got, err := s.Roles(context.Background(), store.RoleFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "reviewer" || got[0].Description != "Reviews diffs." {
		t.Fatalf("roles after reassign = %+v, want only reviewer", got)
	}
	if !got[0].AssignedAt.Equal(replaced.AssignedAt) || got[0].Room != worktreeRoom {
		t.Errorf("role round trip = %+v", got[0])
	}
}

func testAssignValidates(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	bad := role(agentA, worktreeRoom, "Tester")
	if err := s.Assign(context.Background(), bad); err == nil {
		t.Error("Assign accepted an invalid role name")
	}
	if err := s.Assign(context.Background(), role("", worktreeRoom, "tester")); err == nil {
		t.Error("Assign accepted a role with no session")
	}
}

func testDrop(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	ctx := context.Background()
	mustAssign(t, s, role(agentA, worktreeRoom, "tester"))
	mustAssign(t, s, role(agentB, worktreeRoom, "tester"))

	if dropped, err := s.Drop(ctx, agentA); err != nil || !dropped {
		t.Fatalf("Drop = (%v, %v), want dropped", dropped, err)
	}
	if dropped, err := s.Drop(ctx, agentA); err != nil || dropped {
		t.Fatalf("second Drop = (%v, %v), want nothing to drop", dropped, err)
	}
	assertRoles(t, roleNames(t, s, store.RoleFilter{}), agentB+"=tester")
}

func testRolesActiveOnly(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"live", "gone"} {
		sess := &session.Session{ID: id, Harness: session.HarnessCodex, Status: session.StatusActive, StartedAt: now, LastSeen: now}
		if err := s.Upsert(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.End(ctx, "codex:gone", now, "exit"); err != nil {
		t.Fatal(err)
	}
	mustAssign(t, s, role("codex:live", worktreeRoom, "tester"))
	mustAssign(t, s, role("codex:gone", worktreeRoom, "reviewer"))
	mustAssign(t, s, role("codex:unknown", worktreeRoom, "writer"))

	assertRoles(t, roleNames(t, s, store.RoleFilter{ActiveOnly: true}), "codex:live=tester")
	if all := roleNames(t, s, store.RoleFilter{}); len(all) != 3 {
		t.Errorf("all roles = %v, want 3", all)
	}
}

func testRolesRoomFilter(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	mustAssign(t, s, role(agentA, worktreeRoom, "tester"))
	mustAssign(t, s, role(agentB, repoRoom, "reviewer"))

	assertRoles(t, roleNames(t, s, store.RoleFilter{Rooms: []string{repoRoom}}), agentB+"=reviewer")
	assertRoles(t, roleNames(t, s, store.RoleFilter{Rooms: []string{"/nowhere"}}))
}

func testSkillDirs(t *testing.T, newStore RoleFactory) {
	s := newStore(t)
	ctx := context.Background()
	for _, dir := range []string{"/b", "/a", "/b"} {
		if err := s.RecordSkillDir(ctx, dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ForgetSkillDir(ctx, "/a"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.SkillDirs(ctx); err != nil || len(got) != 1 || got[0] != "/b" {
		t.Fatalf("SkillDirs = (%v, %v), want [/b]", got, err)
	}
}
