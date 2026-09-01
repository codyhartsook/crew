package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// RoomFactory builds an empty room store for one subtest.
type RoomFactory func(t *testing.T) store.RoomStore

// RunRooms executes the room conformance suite against an implementation.
func RunRooms(t *testing.T, newStore RoomFactory) {
	t.Helper()
	tests := map[string]func(*testing.T, RoomFactory){
		"PostAssignsMonotonicIDs": testPostMonotonic,
		"PostValidates":           testPostValidates,
		"EntryFilters":            testEntryFilters,
		"Resolution":              testResolution,
		"Membership":              testMembership,
		"UnreadRespectsRooms":     testUnreadRespectsRooms,
		"UnreadExcludesAuthor":    testUnreadExcludesAuthor,
		"UnreadOnlyAddressed":     testUnreadOnlyAddressed,
		"UnreadStopsAtResolved":   testUnreadStopsAtResolved,
		"UnreadDeliversAnswers":   testUnreadDeliversAnswers,
		"AckMovesForwardOnly":     testAckMovesForwardOnly,
		"StateOverwrites":         testStateOverwrites,
		"StateFilters":            testStateFilters,
		"StateValidates":          testStateValidates,
		"Promote":                 testPromote,
		"Search":                  testSearch,
		"Clear":                   testClear,
		"RemoveEntry":             testRemoveEntry,
		"ConcurrentPost":          testConcurrentPost,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

const (
	worktreeRoom = "/pool/widget-abc/3/widget"
	repoRoom     = "/src/widget"
	agentA       = "codex:a"
	agentB       = "claude:b"
)

func entry(roomKey string, kind room.Kind, author, body string) *room.Entry {
	return &room.Entry{
		Room:      roomKey,
		Scope:     room.ScopeWorktree,
		Kind:      kind,
		Author:    author,
		Body:      body,
		CreatedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
	}
}

func mustPost(t *testing.T, s store.RoomStore, e *room.Entry) *room.Entry {
	t.Helper()
	if err := s.Post(context.Background(), e); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if e.ID == 0 {
		t.Fatal("Post did not assign an id")
	}
	return e
}

func mustJoin(t *testing.T, s store.RoomStore, sessionKey, roomKey string) {
	t.Helper()
	err := s.Join(context.Background(), &room.Membership{
		SessionKey: sessionKey, Room: roomKey,
		Scope: room.ScopeWorktree, JoinedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
}

func testPostMonotonic(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	// Ids must rise across rooms, not just within one: a single per-session
	// cursor tracks delivery over every room the session has joined.
	first := mustPost(t, s, entry(worktreeRoom, room.KindDecision, agentA, "one"))
	second := mustPost(t, s, entry(repoRoom, room.KindFinding, agentB, "two"))
	third := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "three"))

	if !(first.ID < second.ID && second.ID < third.ID) {
		t.Errorf("ids %d, %d, %d are not increasing across rooms", first.ID, second.ID, third.ID)
	}
}

func testPostValidates(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	cases := map[string]*room.Entry{
		"unknown kind": {Room: worktreeRoom, Kind: "gossip", Author: agentA, Body: "x"},
		"empty body":   {Room: worktreeRoom, Kind: room.KindFinding, Author: agentA, Body: "   "},
		"no room":      {Kind: room.KindFinding, Author: agentA, Body: "x"},
		"no author":    {Room: worktreeRoom, Kind: room.KindFinding, Body: "x"},
	}
	for name, e := range cases {
		if err := s.Post(ctx, e); err == nil {
			t.Errorf("Post accepted %s", name)
		}
	}
}

func testEntryFilters(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	decision := mustPost(t, s, entry(worktreeRoom, room.KindDecision, agentA, "chose sqlite"))
	question := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "who owns retries?"))
	elsewhere := mustPost(t, s, entry(repoRoom, room.KindFinding, agentB, "auth ignores ctx"))

	cases := []struct {
		name   string
		filter room.Filter
		want   []int64
	}{
		{"all", room.Filter{}, []int64{decision.ID, question.ID, elsewhere.ID}},
		{"by room", room.Filter{Rooms: []string{worktreeRoom}}, []int64{decision.ID, question.ID}},
		{"by kind", room.Filter{Kinds: []room.Kind{room.KindFinding}}, []int64{elsewhere.ID}},
		{"open only", room.Filter{OpenOnly: true}, []int64{question.ID}},
		{"limit", room.Filter{Limit: 2}, []int64{decision.ID, question.ID}},
		{"both rooms", room.Filter{Rooms: []string{worktreeRoom, repoRoom}}, []int64{decision.ID, question.ID, elsewhere.ID}},
		{"no match", room.Filter{Rooms: []string{"/nowhere"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Entries(ctx, tc.filter)
			if err != nil {
				t.Fatalf("Entries: %v", err)
			}
			assertIDs(t, got, tc.want)
		})
	}
}

func testResolution(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	question := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "who owns retries?"))

	open, err := s.Entries(ctx, room.Filter{OpenOnly: true})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	assertIDs(t, open, []int64{question.ID})

	// Resolution is an append, never a rewrite, so two agents can answer
	// without contending for the same row.
	answer := entry(worktreeRoom, room.KindQuestion, agentB, "the gateway does")
	answer.Resolves = question.ID
	mustPost(t, s, answer)

	open, err = s.Entries(ctx, room.Filter{OpenOnly: true})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("open entries = %v, want none after the question was answered", ids(open))
	}

	all, err := s.Entries(ctx, room.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if all[0].ResolvedBy != answer.ID {
		t.Errorf("ResolvedBy = %d, want %d", all[0].ResolvedBy, answer.ID)
	}
	if all[0].Open() {
		t.Error("Open() = true on an answered question")
	}
}

func testMembership(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	mustJoin(t, s, agentA, worktreeRoom)
	mustJoin(t, s, agentA, repoRoom)
	mustJoin(t, s, agentB, worktreeRoom)

	mustJoin(t, s, agentA, worktreeRoom)

	rooms, err := s.Rooms(ctx, agentA)
	if err != nil {
		t.Fatalf("Rooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Errorf("agent A is in %d rooms, want 2", len(rooms))
	}

	members, err := s.Members(ctx, worktreeRoom)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("worktree room has %d members, want 2", len(members))
	}

	if err := s.Leave(ctx, agentB, worktreeRoom); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	members, err = s.Members(ctx, worktreeRoom)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 1 || members[0].SessionKey != agentA {
		t.Errorf("members = %v, want only agent A", members)
	}

	if err := s.Leave(ctx, agentB, worktreeRoom); err != nil {
		t.Errorf("Leave on a non-member: %v", err)
	}
}

func testUnreadRespectsRooms(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()
	mustJoin(t, s, agentB, worktreeRoom)

	here := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "in a room B joined"))
	mustPost(t, s, entry(repoRoom, room.KindQuestion, agentA, "in a room B did not join"))

	unread, err := s.Unread(ctx, agentB)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	assertIDs(t, unread, []int64{here.ID})

	if err := s.Ack(ctx, agentB, here.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	unread, err = s.Unread(ctx, agentB)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("unread = %v, want nothing after acking", ids(unread))
	}
}

func testUnreadExcludesAuthor(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	mustJoin(t, s, agentA, worktreeRoom)
	mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "my own question"))

	unread, err := s.Unread(context.Background(), agentA)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("unread = %v, want an agent not to be told about its own entry", ids(unread))
	}
}

// Decisions and findings are reference material read on arrival, not messages
// pushed at the other members.
func testUnreadOnlyAddressed(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	mustJoin(t, s, agentB, worktreeRoom)

	mustPost(t, s, entry(worktreeRoom, room.KindDecision, agentA, "chose sqlite"))
	mustPost(t, s, entry(worktreeRoom, room.KindFinding, agentA, "auth ignores ctx"))
	question := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "who owns retries?"))
	handoff := mustPost(t, s, entry(worktreeRoom, room.KindHandoff, agentA, "tests remain"))
	review := mustPost(t, s, entry(worktreeRoom, room.KindReview, agentA, "this leaks a goroutine"))

	unread, err := s.Unread(context.Background(), agentB)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	assertIDs(t, unread, []int64{question.ID, handoff.ID, review.ID})
}

func testUnreadStopsAtResolved(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()
	mustJoin(t, s, agentB, worktreeRoom)

	question := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "who owns retries?"))
	answer := entry(worktreeRoom, room.KindQuestion, agentA, "never mind, the gateway does")
	answer.Resolves = question.ID
	mustPost(t, s, answer)

	unread, err := s.Unread(ctx, agentB)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("unread = %v, want a question answered before delivery to stay undelivered", ids(unread))
	}
}

// Asking a question and never being told it was answered makes the room
// useless for the agent that most needed the reply.
func testUnreadDeliversAnswers(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()
	mustJoin(t, s, agentA, worktreeRoom)
	mustJoin(t, s, agentB, worktreeRoom)

	question := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "who owns retries?"))
	// A has already seen its own question in the briefing.
	if err := s.Ack(ctx, agentA, question.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	answer := entry(worktreeRoom, room.KindQuestion, agentB, "the gateway does")
	answer.Resolves = question.ID
	mustPost(t, s, answer)

	unread, err := s.Unread(ctx, agentA)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	assertIDs(t, unread, []int64{answer.ID})

	// B, who wrote the answer, is not told about it.
	unread, err = s.Unread(ctx, agentB)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("unread for the answering agent = %v, want none", ids(unread))
	}

	// An answer to somebody else's question is not everyone's business.
	mustJoin(t, s, "claude:c", worktreeRoom)
	if err := s.Ack(ctx, "claude:c", question.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	unread, err = s.Unread(ctx, "claude:c")
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("unread for a bystander = %v, want none", ids(unread))
	}
}

func testAckMovesForwardOnly(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()
	mustJoin(t, s, agentB, worktreeRoom)

	first := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "first"))
	second := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "second"))

	if err := s.Ack(ctx, agentB, second.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	// A late ack from a slower turn must not rewind the cursor and redeliver.
	if err := s.Ack(ctx, agentB, first.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	unread, err := s.Unread(ctx, agentB)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("unread = %v, want the cursor to stay at the newer ack", ids(unread))
	}
}

func testConcurrentPost(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	const n = 16
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.Post(ctx, entry(worktreeRoom, room.KindFinding, agentA, fmt.Sprintf("finding %d", i)))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Post: %v", err)
		}
	}

	got, err := s.Entries(ctx, room.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(got) != n {
		t.Fatalf("Entries returned %d, want %d", len(got), n)
	}
	// Every id must be distinct, or a cursor would skip somebody's entry.
	seen := map[int64]bool{}
	for _, e := range got {
		if seen[e.ID] {
			t.Fatalf("duplicate entry id %d", e.ID)
		}
		seen[e.ID] = true
	}
}

func state(roomKey, key, value, author string) *room.State {
	return &room.State{
		Room: roomKey, Scope: room.ScopeWorktree, Key: key, Value: value,
		Author: author, UpdatedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
}

// State is the half of a room that is meant to be replaced, so a reader learns
// the current value without folding a history.
func testStateOverwrites(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	first := state(worktreeRoom, "migration/status", "tables done, indexes pending", agentA)
	if err := s.SetState(ctx, first); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if first.Revision != 1 {
		t.Errorf("revision = %d, want 1 on first write", first.Revision)
	}

	second := state(worktreeRoom, "migration/status", "complete", agentB)
	second.UpdatedAt = first.UpdatedAt.Add(time.Hour)
	if err := s.SetState(ctx, second); err != nil {
		t.Fatalf("SetState again: %v", err)
	}
	if second.Revision != 2 {
		t.Errorf("revision = %d, want 2 on overwrite", second.Revision)
	}

	got, err := s.GetState(ctx, worktreeRoom, "migration/status")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got.Value != "complete" {
		t.Errorf("value = %q, want the newer one", got.Value)
	}
	if got.Author != agentB {
		t.Errorf("author = %q, want the last writer", got.Author)
	}
	if !got.UpdatedAt.Equal(second.UpdatedAt) {
		t.Errorf("updated_at = %v, want %v", got.UpdatedAt, second.UpdatedAt)
	}

	// One key, one row: overwriting must not accumulate.
	all, err := s.States(ctx, room.StateFilter{})
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("States returned %d rows, want 1 after an overwrite", len(all))
	}

	if err := s.DeleteState(ctx, worktreeRoom, "migration/status"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	if _, err := s.GetState(ctx, worktreeRoom, "migration/status"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetState after delete: err = %v, want store.ErrNotFound", err)
	}
	if err := s.DeleteState(ctx, worktreeRoom, "migration/status"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("DeleteState on an absent key: err = %v, want store.ErrNotFound", err)
	}
}

func testStateFilters(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	for _, st := range []*room.State{
		state(worktreeRoom, "build/status", "green", agentA),
		state(worktreeRoom, "build/owner", "codex", agentA),
		state(worktreeRoom, "migration/status", "pending", agentA),
		state(repoRoom, "build/status", "elsewhere", agentB),
	} {
		if err := s.SetState(ctx, st); err != nil {
			t.Fatalf("SetState: %v", err)
		}
	}

	// Keys namespace themselves by prefix, so no column is needed for it.
	got, err := s.States(ctx, room.StateFilter{Rooms: []string{worktreeRoom}, Prefix: "build/"})
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("prefix match returned %d, want 2: %+v", len(got), got)
	}
	if got[0].Key != "build/owner" || got[1].Key != "build/status" {
		t.Errorf("keys = %q, %q, want them ordered", got[0].Key, got[1].Key)
	}

	// The same key in two rooms is two values.
	got, err = s.States(ctx, room.StateFilter{Prefix: "build/status"})
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("same key across rooms returned %d, want 2", len(got))
	}

	// A prefix containing a LIKE wildcard must not match everything.
	if err := s.SetState(ctx, state(worktreeRoom, "a_b", "underscore", agentA)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	got, err = s.States(ctx, room.StateFilter{Rooms: []string{worktreeRoom}, Prefix: "a_"})
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	if len(got) != 1 || got[0].Key != "a_b" {
		t.Errorf("underscore prefix matched %+v, want only a_b", got)
	}
}

func testStateValidates(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	bad := map[string]*room.State{
		"empty key":      state(worktreeRoom, "   ", "x", agentA),
		"key with space": state(worktreeRoom, "two words", "x", agentA),
		"no author":      state(worktreeRoom, "k", "x", ""),
		"no room":        state("", "k", "x", agentA),
	}
	for name, st := range bad {
		if err := s.SetState(ctx, st); err == nil {
			t.Errorf("SetState accepted %s", name)
		}
	}
	// An empty value is legitimate: it can mean "known to be nothing".
	if err := s.SetState(ctx, state(worktreeRoom, "k", "", agentA)); err != nil {
		t.Errorf("SetState rejected an empty value: %v", err)
	}
}

// A question and its answer must never end up in different rooms.
func testPromote(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	question := mustPost(t, s, entry(worktreeRoom, room.KindQuestion, agentA, "repo-wide?"))
	answer := entry(worktreeRoom, room.KindQuestion, agentB, "yes")
	answer.Resolves = question.ID
	mustPost(t, s, answer)
	staying := mustPost(t, s, entry(worktreeRoom, room.KindFinding, agentA, "local to this worktree"))

	moved, err := s.Promote(ctx, question.ID, repoRoom, room.ScopeRepo)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if moved != 2 {
		t.Errorf("moved %d rows, want the question and its answer", moved)
	}
	inRepo, err := s.Entries(ctx, room.Filter{Rooms: []string{repoRoom}})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	assertIDs(t, inRepo, []int64{question.ID, answer.ID})
	if inRepo[0].Scope != room.ScopeRepo {
		t.Errorf("scope = %q, want %q", inRepo[0].Scope, room.ScopeRepo)
	}
	left, err := s.Entries(ctx, room.Filter{Rooms: []string{worktreeRoom}})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	assertIDs(t, left, []int64{staying.ID})

	if _, err := s.Promote(ctx, 9999, repoRoom, room.ScopeRepo); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("promoting an unknown entry: err = %v, want store.ErrNotFound", err)
	}

	// State moves, and refuses to clobber a value already there.
	if err := s.SetState(ctx, state(worktreeRoom, "build/flake", "arm64", agentA)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.PromoteState(ctx, worktreeRoom, "build/flake", repoRoom, room.ScopeRepo); err != nil {
		t.Fatalf("PromoteState: %v", err)
	}
	if _, err := s.GetState(ctx, repoRoom, "build/flake"); err != nil {
		t.Errorf("GetState in the destination: %v", err)
	}
	if _, err := s.GetState(ctx, worktreeRoom, "build/flake"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the value is still in the origin room: %v", err)
	}

	if err := s.SetState(ctx, state(worktreeRoom, "build/flake", "a different local value", agentA)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.PromoteState(ctx, worktreeRoom, "build/flake", repoRoom, room.ScopeRepo); err == nil {
		t.Error("PromoteState clobbered a value already in the destination")
	}
}

func testSearch(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	runbook := state(worktreeRoom, "procedure/cluster-setup", "1. kind create cluster\n2. helm install", agentA)
	if err := s.SetState(ctx, runbook); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.SetState(ctx, state(worktreeRoom, "build/status", "green", agentA)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.SetState(ctx, state(repoRoom, "other/cluster", "elsewhere", agentA)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	relevant := mustPost(t, s, entry(worktreeRoom, room.KindFinding, agentB, "kind needs --config or the controller cannot reach the cluster"))
	mustPost(t, s, entry(worktreeRoom, room.KindDecision, agentB, "unrelated to the topic"))

	got, err := s.Search(ctx, room.Query{Text: "cluster", Rooms: []string{worktreeRoom}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.State) != 1 || got.State[0].Key != "procedure/cluster-setup" {
		t.Errorf("state hits = %+v, want the runbook from this room only", got.State)
	}
	assertIDs(t, got.Entries, []int64{relevant.ID})

	// Without a room filter, other rooms are in scope.
	got, err = s.Search(ctx, room.Query{Text: "cluster"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.State) != 2 {
		t.Errorf("unscoped state hits = %d, want 2", len(got.State))
	}

	// Naming the thing outranks merely mentioning it.
	if err := s.SetState(ctx, state(worktreeRoom, "notes", "mentions cluster in passing", agentA)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	got, err = s.Search(ctx, room.Query{Text: "cluster", Rooms: []string{worktreeRoom}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.State[0].Key != "procedure/cluster-setup" {
		t.Errorf("first hit = %q, want the key match ranked above the value match", got.State[0].Key)
	}

	// A term matching nothing is not an error; an empty term is.
	got, err = s.Search(ctx, room.Query{Text: "nothingmatchesthis"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !got.Empty() {
		t.Errorf("results = %+v, want none", got)
	}
	if _, err := s.Search(ctx, room.Query{Text: "  "}); err == nil {
		t.Error("Search accepted an empty term")
	}
}

func testClear(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()
	mustPost(t, s, entry(worktreeRoom, room.KindDecision, agentA, "one"))
	kept := mustPost(t, s, entry(repoRoom, room.KindFinding, agentB, "elsewhere"))

	n, err := s.Clear(ctx, worktreeRoom)
	if err != nil || n != 1 {
		t.Fatalf("Clear = (%d, %v), want (1, nil)", n, err)
	}
	entries, err := s.Entries(ctx, room.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	assertIDs(t, entries, []int64{kept.ID})
}

func testRemoveEntry(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()
	removable := mustPost(t, s, entry(worktreeRoom, room.KindFinding, agentA, "wrong"))
	other := mustPost(t, s, entry(worktreeRoom, room.KindFinding, agentB, "keep"))
	answered := mustPost(t, s, entry(worktreeRoom, room.KindFinding, agentA, "answered"))
	answer := entry(worktreeRoom, room.KindFinding, agentB, "reply")
	answer.Resolves = answered.ID
	mustPost(t, s, answer)

	if removed, err := s.RemoveEntry(ctx, removable.ID, agentA); err != nil || !removed {
		t.Fatalf("RemoveEntry = (%v, %v), want (true, nil)", removed, err)
	}
	for _, tc := range []struct {
		name   string
		id     int64
		author string
	}{
		{"other author", other.ID, agentA},
		{"has reply", answered.ID, agentA},
		{"is reply", answer.ID, agentB},
		{"missing", 9999, agentA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if removed, err := s.RemoveEntry(ctx, tc.id, tc.author); err != nil || removed {
				t.Errorf("RemoveEntry = (%v, %v), want (false, nil)", removed, err)
			}
		})
	}
	entries, err := s.Entries(ctx, room.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	assertIDs(t, entries, []int64{other.ID, answered.ID, answer.ID})
}

func ids(entries []*room.Entry) []int64 {
	out := make([]int64, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func assertIDs(t *testing.T, got []*room.Entry, want []int64) {
	t.Helper()
	gotIDs := ids(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("ids = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("ids = %v, want %v", gotIDs, want)
		}
	}
}
