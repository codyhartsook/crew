package notify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// DefaultInterval is how often the broker sweeps. Local SQLite reads are cheap,
// and a short interval keeps Claude channel delivery responsive.
const DefaultInterval = time.Second

// Reader is the store surface the broker needs.
type Reader interface {
	Ender
	List(ctx context.Context, f store.Filter) ([]*session.Session, error)
	Unread(ctx context.Context, sessionKey string) ([]*room.Entry, error)
}

// Broker wakes sessions that have entries addressed to them and have not been
// told yet.
//
// Loop suppression comes from three places. Unread excludes a session's own
// entries, so posting never wakes the author. The per-session cursor below
// stops a repeat wake while the agent has still not read what it was told
// about. And a sweep wakes a session at most once, so the interval bounds the
// rate no matter how fast entries arrive.
type Broker struct {
	store    Reader
	log      *slog.Logger
	interval time.Duration
	trigger  chan struct{}

	// woken is the highest entry id each session has been woken for. It is
	// touched only by Run's goroutine, so it needs no lock. Losing it on
	// restart is harmless: a session with genuinely unread entries is woken
	// once more, which is true rather than noisy.
	woken map[string]int64
	// notify is the harness call, injected so tests do not shell out.
	notify notifyFunc
}

func NewBroker(st Reader, log *slog.Logger, interval time.Duration) *Broker {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Broker{
		store:    st,
		log:      log,
		interval: interval,
		trigger:  make(chan struct{}, 1),
		woken:    map[string]int64{},
		notify:   harness.NotifySession,
	}
}

// Trigger requests an immediate sweep. Signals coalesce because one sweep sees
// every committed entry.
func (b *Broker) Trigger() {
	select {
	case b.trigger <- struct{}{}:
	default:
	}
}

// Run sweeps until ctx is cancelled.
func (b *Broker) Run(ctx context.Context) {
	t := time.NewTicker(b.interval)
	defer t.Stop()
	b.log.Info("notify broker running", "interval", b.interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.trigger:
			b.sweep(ctx)
		case <-t.C:
			b.sweep(ctx)
		}
	}
}

func (b *Broker) sweep(ctx context.Context) {
	sessions, err := b.store.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		b.log.Error("notify sweep", "err", err)
		return
	}
	// Names for the notice. Active sessions cover the case that matters, an
	// agent posting to a live agent; anything else falls back to the key.
	live := map[string]bool{}
	authors := room.Authors{}
	for _, s := range sessions {
		live[s.Key()] = true
		if s.Alias != "" {
			authors[s.Key()] = s.Alias
		}
	}
	for _, s := range sessions {
		if !harness.CanNotify(s.Harness) {
			continue
		}
		if err := b.sweepSession(ctx, s, authors); err != nil {
			b.log.Warn("notify", "session", s.Key(), "err", err)
		}
	}
	// Drop cursors for sessions that ended, so the map cannot grow without
	// bound and a reused key starts clean.
	for key := range b.woken {
		if !live[key] {
			delete(b.woken, key)
		}
	}
}

func (b *Broker) sweepSession(ctx context.Context, s *session.Session, authors room.Authors) error {
	unread, err := b.store.Unread(ctx, s.Key())
	if err != nil || len(unread) == 0 {
		return err
	}
	newest := unread[len(unread)-1].ID
	if b.woken[s.Key()] >= newest {
		return nil
	}
	if err := deliver(ctx, b.store, b.notify, s, noticeText(unread, authors)); err != nil {
		return err
	}
	b.woken[s.Key()] = newest
	b.log.Info("woke session", "session", s.Key(), "entries", len(unread), "through", newest)
	return nil
}

// noticeText names what is waiting without reproducing it, so the agent reads
// the room and acks rather than acting on a copy that may already be stale.
// The sender is named, never keyed: an agent should not have to look up who
// wrote to it.
func noticeText(unread []*room.Entry, authors room.Authors) string {
	newest := unread[len(unread)-1]
	from := authors.Name(newest.Author)
	if len(unread) == 1 {
		return fmt.Sprintf("crew: %s [%d] from %s is addressed to you. Read it with: crew room --inbox --ack",
			newest.Kind, newest.ID, from)
	}
	return fmt.Sprintf("crew: %d entries are addressed to you, newest %s [%d] from %s. Read them with: crew room --inbox --ack",
		len(unread), newest.Kind, newest.ID, from)
}
