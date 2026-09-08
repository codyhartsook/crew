package storetest

import (
	"context"
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
		"UnreadHonorsTarget":      testUnreadHonorsTarget,
		"UnreadStopsAtResolved":   testUnreadStopsAtResolved,
		"UnreadDeliversAnswers":   testUnreadDeliversAnswers,
		"AckMovesForwardOnly":     testAckMovesForwardOnly,
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

func entry(roomKey string, kind room.Mode, author, body string) *room.Entry {
	return &room.Entry{
		Room:      roomKey,
		Scope:     room.ScopeWorktree,
		Mode:      kind,
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
	first := mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentA, "one"))
	second := mustPost(t, s, entry(repoRoom, room.ModeNote, agentB, "two"))
	third := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "three"))

	if !(first.ID < second.ID && second.ID < third.ID) {
		t.Errorf("ids %d, %d, %d are not increasing across rooms", first.ID, second.ID, third.ID)
	}
}

func testPostValidates(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	cases := map[string]*room.Entry{
		"unknown kind":  {Room: worktreeRoom, Mode: "gossip", Author: agentA, Body: "x"},
		"empty body":    {Room: worktreeRoom, Mode: room.ModeNote, Author: agentA, Body: "   "},
		"no room":       {Mode: room.ModeNote, Author: agentA, Body: "x"},
		"no author":     {Room: worktreeRoom, Mode: room.ModeNote, Body: "x"},
		"targeted fact": {Room: worktreeRoom, Mode: room.ModeNote, Author: agentA, To: agentB, Body: "x"},
		"targets self":  {Room: worktreeRoom, Mode: room.ModeRequest, Author: agentA, To: agentA, Body: "x"},
	}
	for name, e := range cases {
		if err := s.Post(ctx, e); err == nil {
			t.Errorf("Post accepted %s", name)
		}
	}
}

func testUnreadHonorsTarget(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	mustJoin(t, s, agentB, worktreeRoom)
	mustJoin(t, s, "claude:c", worktreeRoom)
	targeted := entry(worktreeRoom, room.ModeRequest, agentA, "for B")
	targeted.To = agentB
	mustPost(t, s, targeted)

	for key, want := range map[string][]int64{agentB: {targeted.ID}, "claude:c": nil} {
		got, err := s.Unread(context.Background(), key)
		if err != nil {
			t.Fatalf("Unread(%s): %v", key, err)
		}
		assertIDs(t, got, want)
	}
}

func testEntryFilters(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	decision := mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentA, "chose sqlite"))
	question := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "who owns retries?"))
	elsewhere := mustPost(t, s, entry(repoRoom, room.ModeNote, agentB, "auth ignores ctx"))

	cases := []struct {
		name   string
		filter room.Filter
		want   []int64
	}{
		{"all", room.Filter{}, []int64{decision.ID, question.ID, elsewhere.ID}},
		{"by room", room.Filter{Rooms: []string{worktreeRoom}}, []int64{decision.ID, question.ID}},
		{"by kind", room.Filter{Modes: []room.Mode{room.ModeNote}}, []int64{decision.ID, elsewhere.ID}},
		{"open only", room.Filter{OpenOnly: true}, []int64{question.ID}},
		// A limit keeps the newest entries, still oldest first. Truncating the
		// other way would freeze a briefing on the first entries a room ever got.
		{"limit", room.Filter{Limit: 2}, []int64{question.ID, elsewhere.ID}},
		{"limit keeps the newest", room.Filter{Limit: 1}, []int64{elsewhere.ID}},
		{"limit above the total", room.Filter{Limit: 9}, []int64{decision.ID, question.ID, elsewhere.ID}},
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

	question := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "who owns retries?"))

	open, err := s.Entries(ctx, room.Filter{OpenOnly: true})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	assertIDs(t, open, []int64{question.ID})

	// Resolution is an append, never a rewrite, so two agents can answer
	// without contending for the same row.
	answer := entry(worktreeRoom, room.ModeRequest, agentB, "the gateway does")
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

	here := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "in a room B joined"))
	mustPost(t, s, entry(repoRoom, room.ModeRequest, agentA, "in a room B did not join"))

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
	mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "my own question"))

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

	mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentA, "chose sqlite"))
	mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentA, "auth ignores ctx"))
	question := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "who owns retries?"))
	handoff := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "tests remain"))
	review := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "this leaks a goroutine"))

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

	question := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "who owns retries?"))
	answer := entry(worktreeRoom, room.ModeRequest, agentA, "never mind, the gateway does")
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

	question := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "who owns retries?"))
	// A has already seen its own question in the briefing.
	if err := s.Ack(ctx, agentA, question.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	answer := entry(worktreeRoom, room.ModeRequest, agentB, "the gateway does")
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

	first := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "first"))
	second := mustPost(t, s, entry(worktreeRoom, room.ModeRequest, agentA, "second"))

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
			errs <- s.Post(ctx, entry(worktreeRoom, room.ModeNote, agentA, fmt.Sprintf("finding %d", i)))
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

func testSearch(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()

	relevant := mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentB, "kind needs --config or the controller cannot reach the cluster"))
	mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentB, "unrelated to the topic"))
	mustPost(t, s, entry(repoRoom, room.ModeNote, agentA, "cluster elsewhere"))

	got, err := s.Search(ctx, room.Query{Text: "cluster", Rooms: []string{worktreeRoom}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertIDs(t, got, []int64{relevant.ID})

	// Without a room filter, other rooms are in scope.
	got, err = s.Search(ctx, room.Query{Text: "cluster"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("unscoped hits = %d, want 2", len(got))
	}

	// A term matching nothing is not an error; an empty term is.
	got, err = s.Search(ctx, room.Query{Text: "nothingmatchesthis"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("results = %+v, want none", got)
	}
	if _, err := s.Search(ctx, room.Query{Text: "  "}); err == nil {
		t.Error("Search accepted an empty term")
	}
}

func testClear(t *testing.T, newStore RoomFactory) {
	s := newStore(t)
	ctx := context.Background()
	mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentA, "one"))
	kept := mustPost(t, s, entry(repoRoom, room.ModeNote, agentB, "elsewhere"))

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
	removable := mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentA, "wrong"))
	other := mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentB, "keep"))
	answered := mustPost(t, s, entry(worktreeRoom, room.ModeNote, agentA, "answered"))
	answer := entry(worktreeRoom, room.ModeNote, agentB, "reply")
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
