package server

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/cli/skill"
	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/thread"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/prune"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// livenessInterval is longer than the usage ticker: this sweep flocks every
// open thread's lock file and is not needed at 1s cadence.
const livenessInterval = 10 * time.Second

// livenessCoordinator keeps the registry's active sessions matched to what
// each harness actually has open: it reaps sessions with no open thread,
// adopts open threads the registry never saw start, and refreshes last_seen
// from the conversation file rather than only the prompt hook.
//
// store.Store, not a narrower surface: adoption goes through hook.Recorder,
// which needs the whole thing.
type livenessCoordinator struct {
	store store.Store
	log   *slog.Logger
	host  string
	// skillsHome holds the legacy global skill dirs; empty skips the sync, as in tests.
	skillsHome string
}

func newLivenessCoordinator(st store.Store, log *slog.Logger) *livenessCoordinator {
	host, _ := os.Hostname()
	return &livenessCoordinator{store: st, log: log, host: host}
}

func (c *livenessCoordinator) run(ctx context.Context) {
	c.sweep(ctx)
	t := time.NewTicker(livenessInterval)
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

func (c *livenessCoordinator) sweep(ctx context.Context) {
	c.reap(ctx)
	c.syncRoles(ctx)
	c.reconcileThreads(ctx)
}

// syncRoles clears crashed holders' skills and writes ones a sandbox blocked.
func (c *livenessCoordinator) syncRoles(ctx context.Context) {
	if c.skillsHome == "" {
		return
	}
	if err := skill.SyncRoles(ctx, c.skillsHome, c.store); err != nil {
		c.log.Warn("liveness role skills", "err", err)
	}
}

// reap ends sessions the prune rule finds dead, thread-aware where a harness
// reports one.
func (c *livenessCoordinator) reap(ctx context.Context) {
	dead, err := prune.Dead(ctx, c.store)
	if err != nil {
		c.log.Warn("liveness reap", "err", err)
		return
	}
	n, err := prune.End(ctx, c.store, dead, time.Now().UTC())
	if err != nil {
		c.log.Warn("liveness end", "err", err)
	} else if n > 0 {
		c.log.Info("liveness reaped", "count", n)
	}
}

// reconcileThreads adopts unregistered live threads and refreshes last_seen
// from each session's conversation file, one harness at a time. Every known
// harness is visited, not just ones with an existing session: adoption's
// whole point is a harness the registry has zero sessions for yet.
func (c *livenessCoordinator) reconcileThreads(ctx context.Context) {
	sessions, err := c.store.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		c.log.Warn("liveness list", "err", err)
		return
	}

	registered := map[string]bool{} // "<harness>:<id>" already active
	byHarness := map[session.Harness][]*session.Session{}
	for _, s := range sessions {
		if c.host != "" && s.Host != "" && s.Host != c.host {
			continue
		}
		registered[s.Key()] = true
		byHarness[s.Harness] = append(byHarness[s.Harness], s)
	}

	for _, spec := range harness.Specs() {
		if spec.Threads == nil {
			c.refreshFromTranscripts(ctx, byHarness[spec.Harness])
			continue
		}
		open, err := spec.Threads.Open()
		if err != nil {
			c.log.Debug("liveness thread source", "harness", spec.Harness, "err", err)
			continue
		}
		c.adopt(ctx, spec.Harness, open, registered)
		c.refreshFromThreads(ctx, byHarness[spec.Harness], open)
	}
}

// adopt registers a session for every held thread a person is driving that
// the registry does not already have active. Filtering on thread_source is
// what keeps a guardian_review or subagent thread off the graph.
func (c *livenessCoordinator) adopt(ctx context.Context, h session.Harness, open map[string]thread.Thread, registered map[string]bool) {
	for id, th := range open {
		if th.Source != "user" {
			continue
		}
		key := string(h) + ":" + id
		if registered[key] {
			continue
		}
		if err := c.register(ctx, h, th); err != nil {
			c.log.Warn("adopt session", "harness", h, "id", id, "err", err)
			continue
		}
		registered[key] = true
		c.log.Info("adopted session", "harness", h, "id", id, "cwd", th.CWD)
	}
}

// register builds the same record a SessionStart hook would, so an adopted
// session gets repo/worktree detection, an alias, and a room, then corrects
// the fields the hook infers from its own process before the single write.
// The pid is one of them: liveness here comes from the thread lock, and the
// pid the hook would infer belongs to this registry, not the thread's holder.
func (c *livenessCoordinator) register(ctx context.Context, h session.Harness, th thread.Thread) error {
	recorder := &hook.Recorder{Store: c.store, Detector: detect.New()}
	payload := hook.Payload{SessionID: th.ID, CWD: th.CWD, HookEventName: string(hook.EventStart), Source: "adopted"}
	sess, err := recorder.Adopt(ctx, h, payload)
	if err != nil {
		c.log.Debug("adopt detect", "id", th.ID, "err", err)
	}

	sess.PID = 0
	sess.Host = c.host
	if !th.Started.IsZero() {
		sess.StartedAt = th.Started
	}
	if !th.Active.IsZero() && th.Active.After(sess.StartedAt) {
		sess.LastSeen = th.Active
	}
	if th.Originator != "" {
		if sess.Meta == nil {
			sess.Meta = map[string]string{}
		}
		sess.Meta["originator"] = th.Originator
	}
	if err := c.store.Upsert(ctx, sess); err != nil {
		return err
	}
	return c.join(ctx, sess)
}

// join adds an adopted session to the room for its place, the same as the
// prompt hook does for a session whose SessionStart actually fired.
func (c *livenessCoordinator) join(ctx context.Context, sess *session.Session) error {
	rs, ok := c.store.(store.RoomStore)
	if !ok {
		return nil
	}
	here := room.For(sess.Place)
	if len(here) == 0 {
		return nil
	}
	return roomctx.Join(ctx, rs, sess.Key(), here)
}

// refreshFromThreads takes last_seen to the later of its stored value and the
// backing rollout's mtime, for harnesses whose thread source names one.
func (c *livenessCoordinator) refreshFromThreads(ctx context.Context, sessions []*session.Session, open map[string]thread.Thread) {
	for _, s := range sessions {
		th, ok := open[s.ID]
		if !ok || th.Path == "" {
			continue
		}
		c.touchIfNewer(ctx, s, th.Active)
	}
}

// refreshFromTranscripts is the same idea for a harness with no thread
// source: last_seen comes from the transcript path the hook recorded.
func (c *livenessCoordinator) refreshFromTranscripts(ctx context.Context, sessions []*session.Session) {
	for _, s := range sessions {
		path := s.Meta["transcript_path"]
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		c.touchIfNewer(ctx, s, info.ModTime())
	}
}

// touchIfNewer moves last_seen forward, never back: mtime is a floor on
// activity, not a substitute for a more recent one already on record.
func (c *livenessCoordinator) touchIfNewer(ctx context.Context, s *session.Session, mtime time.Time) {
	if mtime.IsZero() || !mtime.After(s.LastSeen) {
		return
	}
	if err := c.store.Touch(ctx, s.Key(), mtime); err != nil {
		c.log.Warn("liveness touch", "session", s.Key(), "err", err)
	}
}
