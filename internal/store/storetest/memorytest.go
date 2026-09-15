package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/rolemem"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// MemoryFactory builds an empty memory store for one subtest.
type MemoryFactory func(t *testing.T) store.MemoryStore

// RunMemory executes the role memory conformance suite against an implementation.
func RunMemory(t *testing.T, newStore MemoryFactory) {
	t.Helper()
	tests := map[string]func(*testing.T, MemoryFactory){
		"WriteAssignsMonotonicIDs":      testMemoryWriteMonotonic,
		"WriteValidates":                testMemoryWriteValidates,
		"ReadFiltersByRoomAndRole":      testMemoryReadFilters,
		"ReadOrdersOldestFirst":         testMemoryReadOrder,
		"ReadLimitKeepsNewest":          testMemoryReadLimit,
		"RolesInTheSameRoomAreSeparate": testMemoryRoleIsolation,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

func memEntry(room, role, body string) *rolemem.Entry {
	return &rolemem.Entry{
		Room:      room,
		Role:      role,
		Author:    "codex:a",
		Body:      body,
		CreatedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
	}
}

func mustWriteMemory(t *testing.T, s store.MemoryStore, e *rolemem.Entry) *rolemem.Entry {
	t.Helper()
	if err := s.WriteMemory(context.Background(), e); err != nil {
		t.Fatalf("WriteMemory: %v", err)
	}
	if e.ID == 0 {
		t.Fatal("WriteMemory did not assign an id")
	}
	return e
}

func testMemoryWriteMonotonic(t *testing.T, newStore MemoryFactory) {
	s := newStore(t)
	a := mustWriteMemory(t, s, memEntry("/repo", "tester", "first"))
	b := mustWriteMemory(t, s, memEntry("/repo", "tester", "second"))
	if b.ID <= a.ID {
		t.Errorf("ids = %d, %d, want strictly increasing", a.ID, b.ID)
	}
}

func testMemoryWriteValidates(t *testing.T, newStore MemoryFactory) {
	s := newStore(t)
	cases := []*rolemem.Entry{
		{Role: "tester", Body: "x"},     // no room
		{Room: "/repo", Body: "x"},      // no role
		{Room: "/repo", Role: "tester"}, // no body
		{Room: "/repo", Role: "tester", Body: "   "},
	}
	for _, e := range cases {
		if err := s.WriteMemory(context.Background(), e); err == nil {
			t.Errorf("WriteMemory(%+v) = nil, want an error", e)
		}
	}
}

func testMemoryReadFilters(t *testing.T, newStore MemoryFactory) {
	s := newStore(t)
	mustWriteMemory(t, s, memEntry("/repo-a", "tester", "a-tester"))
	mustWriteMemory(t, s, memEntry("/repo-a", "reviewer", "a-reviewer"))
	mustWriteMemory(t, s, memEntry("/repo-b", "tester", "b-tester"))

	got, err := s.ReadMemory(context.Background(), rolemem.Filter{Room: "/repo-a", Role: "tester"})
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if len(got) != 1 || got[0].Body != "a-tester" {
		t.Errorf("ReadMemory(repo-a, tester) = %+v, want only a-tester", got)
	}
}

func testMemoryReadOrder(t *testing.T, newStore MemoryFactory) {
	s := newStore(t)
	mustWriteMemory(t, s, memEntry("/repo", "tester", "first"))
	mustWriteMemory(t, s, memEntry("/repo", "tester", "second"))
	mustWriteMemory(t, s, memEntry("/repo", "tester", "third"))

	got, err := s.ReadMemory(context.Background(), rolemem.Filter{Room: "/repo", Role: "tester"})
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if len(got) != 3 || got[0].Body != "first" || got[2].Body != "third" {
		t.Errorf("ReadMemory order = %+v, want oldest first", got)
	}
}

func testMemoryReadLimit(t *testing.T, newStore MemoryFactory) {
	s := newStore(t)
	mustWriteMemory(t, s, memEntry("/repo", "tester", "first"))
	mustWriteMemory(t, s, memEntry("/repo", "tester", "second"))
	mustWriteMemory(t, s, memEntry("/repo", "tester", "third"))

	got, err := s.ReadMemory(context.Background(), rolemem.Filter{Room: "/repo", Role: "tester", Limit: 2})
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	// Still oldest first within the kept window, but the window itself keeps
	// the newest entries: a bounded pull is only useful when it is recent.
	if len(got) != 2 || got[0].Body != "second" || got[1].Body != "third" {
		t.Errorf("ReadMemory(limit 2) = %+v, want [second, third]", got)
	}
}

func testMemoryRoleIsolation(t *testing.T, newStore MemoryFactory) {
	s := newStore(t)
	mustWriteMemory(t, s, memEntry("/repo", "tester", "tester-secret"))
	mustWriteMemory(t, s, memEntry("/repo", "reviewer", "reviewer-secret"))

	got, err := s.ReadMemory(context.Background(), rolemem.Filter{Room: "/repo", Role: "tester"})
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	for _, e := range got {
		if e.Body == "reviewer-secret" {
			t.Error("tester's read returned the reviewer's memory")
		}
	}
}
