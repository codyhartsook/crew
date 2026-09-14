package notify

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness/notifier"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

type fakeReader struct {
	sessions []*session.Session
	unread   map[string][]*room.Entry
	asked    []string
	ended    []string
	usageSet map[string]*usage.Snapshot
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

func (f *fakeReader) SetUsage(_ context.Context, key string, u *usage.Snapshot) error {
	if f.usageSet == nil {
		f.usageSet = map[string]*usage.Snapshot{}
	}
	f.usageSet[key] = u
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
		woken: map[string]int64{}, notify: w, lastMaintain: time.Now(),
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

func TestMaintainRetriesUnknownUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "09", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(dir, "rollout-2026-09-02T13-00-13-01a05f19-7894-7b62-be69-92260048f107.jsonl")
	line := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":3000,"cached_input_tokens":2500,"output_tokens":90},"last_token_usage":{"input_tokens":1200,"cached_input_tokens":1100,"output_tokens":40},"model_context_window":258400}}}`
	if err := os.WriteFile(rollout, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sess(session.HarnessCodex, "01a05f19-7831-78a1-9f95-11cd7a51f258")
	f := &fakeReader{sessions: []*session.Session{s}}
	b := &Broker{store: f, log: slog.New(slog.NewTextHandler(io.Discard, nil)), woken: map[string]int64{}}

	b.maintain(context.Background())

	got, ok := f.usageSet[s.Key()]
	if !ok {
		t.Fatal("usage was not retried")
	}
	if got.ContextUsed != 1200 || got.InputTokens != 3000 {
		t.Errorf("snapshot = %+v, want context 1200, input 3000", got)
	}
}

func TestMaintainSkipsSessionsWithKnownUsage(t *testing.T) {
	s := sess(session.HarnessCodex, "t1")
	s.Usage = &usage.Snapshot{InputTokens: 1}
	f := &fakeReader{sessions: []*session.Session{s}}
	b := &Broker{store: f, log: slog.New(slog.NewTextHandler(io.Discard, nil)), woken: map[string]int64{}}

	b.maintain(context.Background())

	if len(f.usageSet) != 0 {
		t.Errorf("usageSet = %v, want no retries for a session with known usage", f.usageSet)
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
