package notify

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/channel"
	"github.com/codyhartsook/multiplayer/internal/harness/notifier"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

type fakeReader struct {
	sessions []*session.Session
	unread   map[string][]*room.Entry
	inbox    map[string][]*channel.Request
	asked    []string
	ended    []string
}

func (f *fakeReader) Inbox(_ context.Context, self channel.Addr) ([]*channel.Request, error) {
	return f.inbox[string(self)], nil
}

func (f *fakeReader) List(context.Context, store.Filter) ([]*session.Session, error) {
	return f.sessions, nil
}

func (f *fakeReader) Unread(_ context.Context, key string) ([]*room.Entry, error) {
	f.asked = append(f.asked, key)
	return f.unread[key], nil
}

func (f *fakeReader) End(_ context.Context, key string, _ time.Time, _ string) error {
	f.ended = append(f.ended, key)
	return nil
}

func entries(ids ...int64) []*room.Entry {
	out := make([]*room.Entry, 0, len(ids))
	for _, id := range ids {
		out = append(out, &room.Entry{ID: id, Mode: room.ModeRequest, Author: "codex:other", Room: "r"})
	}
	return out
}

func testBroker(f *fakeReader, w notifyFunc) *Broker {
	return &Broker{
		store: f, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		woken: map[string]int64{}, asks: f, wokenAsk: map[string]int64{},
		notify: w,
	}
}

func sess(h session.Harness, id string) *session.Session {
	return &session.Session{ID: id, Harness: h, Status: session.StatusActive}
}

func TestSweepWakesOncePerNewEntry(t *testing.T) {
	f := &fakeReader{
		sessions: []*session.Session{sess(session.HarnessCodex, "t1")},
		unread:   map[string][]*room.Entry{"codex:t1": entries(7)},
	}
	var woke int
	b := testBroker(f, func(context.Context, *session.Session, string) error { woke++; return nil })

	b.sweep(context.Background())
	b.sweep(context.Background())
	if woke != 1 {
		t.Fatalf("woke %d times for the same entry, want 1", woke)
	}

	// A newer entry is new information, so it earns one more wake.
	f.unread["codex:t1"] = entries(7, 9)
	b.sweep(context.Background())
	if woke != 2 {
		t.Fatalf("woke %d times, want 2 after a new entry", woke)
	}
}

func TestTriggerSweepsImmediately(t *testing.T) {
	f := &fakeReader{
		sessions: []*session.Session{sess(session.HarnessCodex, "t1")},
		unread:   map[string][]*room.Entry{"codex:t1": entries(7)},
	}
	b := NewBroker(f, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour)
	woke := make(chan struct{}, 1)
	b.notify = func(context.Context, *session.Session, string) error {
		woke <- struct{}{}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)

	b.Trigger()
	select {
	case <-woke:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("trigger did not wake the session immediately")
	}
}

func TestSweepSkipsHarnessWithoutNotifier(t *testing.T) {
	f := &fakeReader{
		sessions: []*session.Session{sess(session.HarnessUnknown, "u1")},
		unread:   map[string][]*room.Entry{"unknown:u1": entries(1)},
	}
	b := testBroker(f, func(context.Context, *session.Session, string) error {
		t.Fatal("woke a harness with no notifier")
		return nil
	})
	b.sweep(context.Background())
	if len(f.asked) != 0 {
		t.Errorf("queried unread for %v, want no queries", f.asked)
	}
}

func TestSweepIgnoresSessionWithNothingUnread(t *testing.T) {
	f := &fakeReader{sessions: []*session.Session{sess(session.HarnessCodex, "t1")}}
	b := testBroker(f, func(context.Context, *session.Session, string) error {
		t.Fatal("woke a session with nothing unread")
		return nil
	})
	b.sweep(context.Background())
}

func TestSweepRetiresGoneSessionAndRetriesNothing(t *testing.T) {
	f := &fakeReader{
		sessions: []*session.Session{sess(session.HarnessCodex, "t1")},
		unread:   map[string][]*room.Entry{"codex:t1": entries(3)},
	}
	b := testBroker(f, func(context.Context, *session.Session, string) error { return notifier.ErrSessionGone })
	b.sweep(context.Background())
	if len(f.ended) != 1 || f.ended[0] != "codex:t1" {
		t.Fatalf("ended %v, want [codex:t1]", f.ended)
	}
	// The cursor must not advance on a failed wake, or a recovered session
	// would never be told.
	if b.woken["codex:t1"] != 0 {
		t.Errorf("cursor advanced to %d on a failed wake", b.woken["codex:t1"])
	}
}

func TestSweepForgetsEndedSessions(t *testing.T) {
	f := &fakeReader{
		sessions: []*session.Session{sess(session.HarnessCodex, "t1")},
		unread:   map[string][]*room.Entry{"codex:t1": entries(4)},
	}
	b := testBroker(f, func(context.Context, *session.Session, string) error { return nil })
	b.sweep(context.Background())
	if b.woken["codex:t1"] == 0 {
		t.Fatal("cursor not recorded")
	}
	f.sessions = nil
	b.sweep(context.Background())
	if len(b.woken) != 0 {
		t.Errorf("kept cursors %v for sessions that are gone", b.woken)
	}
}

func TestWakeTextNamesTheEntry(t *testing.T) {
	named := room.Authors{"codex:other": "coral-lynx"}
	one := noticeText(entries(12), named)
	if !strings.Contains(one, "[12]") || !strings.Contains(one, "request") || !strings.Contains(one, "crew room") {
		t.Errorf("single-entry text lost detail: %q", one)
	}
	many := noticeText(entries(12, 13), named)
	if !strings.Contains(many, "2 entries") || !strings.Contains(many, "[13]") || !strings.Contains(many, "crew room") {
		t.Errorf("multi-entry text lost detail: %q", many)
	}
}

// A woken agent should not have to look up who wrote to it, and an unnamed
// sender must still degrade to something readable rather than a raw key.
func TestWakeTextNamesTheSender(t *testing.T) {
	named := noticeText(entries(12), room.Authors{"codex:other": "coral-lynx"})
	if !strings.Contains(named, "from coral-lynx") {
		t.Errorf("notice did not name the sender: %q", named)
	}
	if strings.Contains(named, "codex:other") {
		t.Errorf("notice leaked the session key: %q", named)
	}
	unnamed := noticeText(entries(12), room.Authors{})
	if strings.Contains(unnamed, "codex:other") {
		t.Errorf("unnamed sender should render as harness and id, got %q", unnamed)
	}
	if !strings.Contains(unnamed, "codex other") {
		t.Errorf("unnamed sender lost its fallback: %q", unnamed)
	}
}

// A delegated role cannot re-ask, so its question must wake the requester -
// once, and again only for a newer question.
func TestSweepWakesForAWaitingQuestion(t *testing.T) {
	f := &fakeReader{
		sessions: []*session.Session{sess(session.HarnessCodex, "t1")},
		inbox: map[string][]*channel.Request{
			"codex:t1": {{ID: 1, From: "claude:role", To: "codex:t1", Body: "which config?"}},
		},
	}
	var texts []string
	b := testBroker(f, func(_ context.Context, _ *session.Session, text string) error {
		texts = append(texts, text)
		return nil
	})

	b.sweep(context.Background())
	b.sweep(context.Background())
	if len(texts) != 1 {
		t.Fatalf("woke %d times for the same question, want 1", len(texts))
	}
	if !strings.Contains(texts[0], "which config?") || !strings.Contains(texts[0], "crew answer") {
		t.Errorf("wake text = %q, want the question and how to answer it", texts[0])
	}

	f.inbox["codex:t1"] = append(f.inbox["codex:t1"], &channel.Request{ID: 2, From: "claude:role", To: "codex:t1", Body: "and the fixture?"})
	b.sweep(context.Background())
	if len(texts) != 2 {
		t.Fatalf("woke %d times, want 2 after a second question", len(texts))
	}
}
