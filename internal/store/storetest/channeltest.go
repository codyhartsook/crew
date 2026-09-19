package storetest

import (
	"context"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/channel"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// ChannelFactory builds an empty channel store for one subtest.
type ChannelFactory func(t *testing.T) store.ChannelStore

// RunChannel executes the back-channel conformance suite against an
// implementation.
func RunChannel(t *testing.T, newStore ChannelFactory) {
	t.Helper()
	tests := map[string]func(*testing.T, ChannelFactory){
		"AskAnswerRoundTrip":     testChannelRoundTrip,
		"AskValidates":           testChannelAskValidates,
		"AnswerOnce":             testChannelAnswerOnce,
		"InboxIsAddressedOnly":   testChannelInboxAddressed,
		"InboxIgnoresAnswered":   testChannelInboxOpenOnly,
		"InboxRefusesNoAddress":  testChannelInboxNoAddress,
		"CloseRequestsFromAsker": testChannelCloseRequests,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

const (
	asker     channel.Addr = "claude:asker"
	requester channel.Addr = "codex:launcher"
	bystander channel.Addr = "codex:other"
)

func testChannelRoundTrip(t *testing.T, newStore ChannelFactory) {
	s := newStore(t)
	ctx := context.Background()

	id, err := s.Ask(ctx, asker, requester, "which config does the test read?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	open, err := s.GetRequest(ctx, id)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if !open.Open() || open.From != asker || open.To != requester {
		t.Fatalf("GetRequest = %+v, want an open request from %s to %s", open, asker, requester)
	}

	answered, err := s.Answer(ctx, id, "testdata/config.yaml")
	if err != nil || !answered {
		t.Fatalf("Answer() = %v, %v, want true, nil", answered, err)
	}
	got, err := s.GetRequest(ctx, id)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if got.Open() || got.Answer != "testdata/config.yaml" || got.AnsweredAt == nil {
		t.Errorf("GetRequest = %+v, want it closed with the answer", got)
	}
}

func testChannelAskValidates(t *testing.T, newStore ChannelFactory) {
	s := newStore(t)
	ctx := context.Background()
	cases := []struct {
		name     string
		from, to channel.Addr
		body     string
	}{
		{"no sender", "", requester, "why"},
		{"no recipient", asker, "", "why"},
		{"itself", asker, asker, "why"},
		{"empty body", asker, requester, "  "},
	}
	for _, tc := range cases {
		if _, err := s.Ask(ctx, tc.from, tc.to, tc.body); err == nil {
			t.Errorf("Ask(%s) = nil, want an error", tc.name)
		}
	}
}

func testChannelAnswerOnce(t *testing.T, newStore ChannelFactory) {
	s := newStore(t)
	ctx := context.Background()
	id, err := s.Ask(ctx, asker, requester, "which config?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(ctx, id, "first"); err != nil {
		t.Fatal(err)
	}
	answered, err := s.Answer(ctx, id, "second")
	if err != nil || answered {
		t.Errorf("second Answer() = %v, %v, want false, nil", answered, err)
	}
	got, err := s.GetRequest(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer != "first" {
		t.Errorf("Answer = %q, want the first answer kept", got.Answer)
	}
	if answered, err := s.Answer(ctx, id+404, "nobody asked"); err != nil || answered {
		t.Errorf("Answer on an unknown id = %v, %v, want false, nil", answered, err)
	}
}

func testChannelInboxAddressed(t *testing.T, newStore ChannelFactory) {
	s := newStore(t)
	ctx := context.Background()
	mine, err := s.Ask(ctx, asker, requester, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ask(ctx, asker, bystander, "not yours"); err != nil {
		t.Fatal(err)
	}

	open, err := s.Inbox(ctx, requester)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(open) != 1 || open[0].ID != mine {
		t.Fatalf("Inbox = %+v, want only [%d]", open, mine)
	}
}

func testChannelInboxOpenOnly(t *testing.T, newStore ChannelFactory) {
	s := newStore(t)
	ctx := context.Background()
	id, err := s.Ask(ctx, asker, requester, "which config?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(ctx, id, "this one"); err != nil {
		t.Fatal(err)
	}
	open, err := s.Inbox(ctx, requester)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("Inbox = %+v, want nothing once answered", open)
	}
}

// The room read leak was an empty filter falling back to unfiltered. Inbox
// must not repeat it.
func testChannelInboxNoAddress(t *testing.T, newStore ChannelFactory) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Ask(ctx, asker, requester, "mine"); err != nil {
		t.Fatal(err)
	}
	open, err := s.Inbox(ctx, "")
	if err != nil {
		t.Fatalf("Inbox(\"\"): %v", err)
	}
	if len(open) != 0 {
		t.Errorf("Inbox(\"\") = %+v, want nothing", open)
	}
}

func testChannelCloseRequests(t *testing.T, newStore ChannelFactory) {
	s := newStore(t)
	ctx := context.Background()
	mine, err := s.Ask(ctx, asker, requester, "mine")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.Ask(ctx, bystander, requester, "theirs")
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.CloseRequests(ctx, asker, "gone")
	if err != nil || n != 1 {
		t.Fatalf("CloseRequests() = %d, %v, want 1, nil", n, err)
	}
	closed, err := s.GetRequest(ctx, mine)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Open() || closed.Answer != "gone" {
		t.Errorf("GetRequest = %+v, want it closed with the note", closed)
	}
	other, err := s.GetRequest(ctx, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if !other.Open() {
		t.Error("another asker's request was closed too")
	}
	if n, err := s.CloseRequests(ctx, "", "gone"); err != nil || n != 0 {
		t.Errorf("CloseRequests(\"\") = %d, %v, want 0, nil", n, err)
	}
}
