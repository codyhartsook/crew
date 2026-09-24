package server

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

const usageInterval = time.Second

type usageStore interface {
	List(context.Context, store.Filter) ([]*session.Session, error)
	SetUsage(context.Context, string, *usage.Snapshot) error
}

type samplerState struct {
	sourceKey string
	sampler   usage.Sampler
}

// usageCoordinator samples local active sessions. The registry owns the
// polling clock; each harness owns the source it reads and how it parses it.
type usageCoordinator struct {
	store    usageStore
	log      *slog.Logger
	host     string
	sources  func(session.Harness) usage.Source
	samplers map[string]samplerState
}

func newUsageCoordinator(st usageStore, log *slog.Logger) *usageCoordinator {
	host, _ := os.Hostname()
	return &usageCoordinator{
		store: st, log: log, host: host,
		sources: func(h session.Harness) usage.Source {
			spec, ok := harness.For(h)
			if !ok {
				return nil
			}
			return spec.Usage
		},
		samplers: map[string]samplerState{},
	}
}

func (c *usageCoordinator) run(ctx context.Context) {
	c.sweep(ctx)
	t := time.NewTicker(usageInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.sweep(ctx)
		}
	}
}

func (c *usageCoordinator) sweep(ctx context.Context) {
	sessions, err := c.store.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		c.log.Error("usage sweep", "err", err)
		return
	}
	live := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		if c.host != "" && sess.Host != "" && sess.Host != c.host {
			continue
		}
		live[sess.Key()] = true
		source := c.sources(sess.Harness)
		if source == nil {
			continue
		}
		transcript := ""
		if sess.Meta != nil {
			transcript = sess.Meta["transcript_path"]
		}
		sourceKey := sess.ID + "\x00" + transcript
		state, ok := c.samplers[sess.Key()]
		if !ok || state.sourceKey != sourceKey {
			state = samplerState{sourceKey: sourceKey, sampler: source.Open(sess.ID, transcript)}
			c.samplers[sess.Key()] = state
		}
		snapshot, changed, err := state.sampler.Refresh(ctx)
		if err != nil {
			c.log.Debug("sample usage", "session", sess.Key(), "err", err)
			continue
		}
		if changed && snapshot.Known() {
			if err := c.store.SetUsage(ctx, sess.Key(), &snapshot); err != nil {
				c.log.Warn("store usage", "session", sess.Key(), "err", err)
			}
		}
	}
	for key := range c.samplers {
		if !live[key] {
			delete(c.samplers, key)
		}
	}
}
