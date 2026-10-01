// Package prune ends sessions whose agent process is gone. A killed harness
// never fires SessionEnd, and only sessions on this machine can be checked.
package prune

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/thread"
	"github.com/codyhartsook/multiplayer/internal/proc"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// deadReason is recorded against sessions closed by pruning rather than by the
// harness, so the two are distinguishable after the fact.
const deadReason = "process gone"

// graceperiod spares recently active sessions: a fresh pid that looks wrong is
// more likely misread than orphaned, and reaping a live session is worse.
const graceperiod = 60 * time.Second

// snapshot reads the process table, injected so tests need not shell out.
var snapshot = proc.Snapshot

// Lister is the store surface Dead needs.
type Lister interface {
	List(ctx context.Context, f store.Filter) ([]*session.Session, error)
}

// Dead lists active sessions on this host whose process is no longer there.
func Dead(ctx context.Context, st Lister) ([]*session.Session, error) {
	active, err := st.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return nil, err
	}
	if len(active) == 0 {
		return nil, nil
	}
	// No process list means cannot tell, not nothing runs: a sandbox that blocks
	// process inspection lands here, and guessing would end live sessions.
	table, err := snapshot()
	if err != nil {
		return nil, fmt.Errorf("read process table: %w", err)
	}
	if len(table) == 0 {
		return nil, errors.New("process table is empty; cannot tell which sessions are alive")
	}
	return deadAmong(active, table, threadSourceFor, detect.Hostname(), time.Now().UTC()), nil
}

// threadSourceFor resolves a harness's thread source from the registry, if it
// has one.
func threadSourceFor(h session.Harness) thread.Source {
	spec, ok := harness.For(h)
	if !ok {
		return nil
	}
	return spec.Threads
}

// deadAmong decides which sessions to reap. Kept separate from reading the
// process table so the policy can be tested without one.
func deadAmong(active []*session.Session, table proc.Table, sourceFor func(session.Harness) thread.Source, host string, now time.Time) []*session.Session {
	live := liveThreadsByHarness(active, sourceFor)

	var dead []*session.Session
	for _, s := range active {
		// A session on another machine cannot be checked at all, by pid or by
		// thread, since both are local-only signals.
		if s.Host != "" && s.Host != host {
			continue
		}
		if now.Sub(s.LastSeen) < graceperiod {
			continue
		}
		if open, ok := live[s.Harness]; ok {
			if !open[s.ID] {
				dead = append(dead, s)
			}
			continue
		}
		// No usable thread source for this harness: fall back to the pid check.
		if s.PID == 0 {
			continue
		}
		if !table.Running(s.PID, binaryOf(s.Harness)) {
			dead = append(dead, s)
		}
	}
	return dead
}

// liveThreadsByHarness maps each harness to its open threads. One left out (no
// source, error, or empty result) falls back to the pid check, not "all died".
func liveThreadsByHarness(active []*session.Session, sourceFor func(session.Harness) thread.Source) map[session.Harness]map[string]bool {
	if sourceFor == nil {
		return nil
	}
	live := map[session.Harness]map[string]bool{}
	seen := map[session.Harness]bool{}
	for _, s := range active {
		if seen[s.Harness] {
			continue
		}
		seen[s.Harness] = true
		src := sourceFor(s.Harness)
		if src == nil {
			continue
		}
		open, err := src.Open()
		if err != nil || len(open) == 0 {
			continue
		}
		ids := make(map[string]bool, len(open))
		for id := range open {
			ids[id] = true
		}
		live[s.Harness] = ids
	}
	return live
}

// Ender is the store surface End needs.
type Ender interface {
	End(ctx context.Context, key string, at time.Time, reason string) error
}

// End closes every session in dead, reporting how many were ended.
func End(ctx context.Context, st Ender, dead []*session.Session, at time.Time) (int, error) {
	n := 0
	for _, s := range dead {
		if err := st.End(ctx, s.Key(), at, deadReason); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Quietly reaps dead sessions without reporting. Used on the start path, where
// a failure to prune must never hold up a session.
func Quietly(ctx context.Context, st store.Store) {
	dead, err := Dead(ctx, st)
	if err != nil || len(dead) == 0 {
		return
	}
	_, _ = End(ctx, st, dead, time.Now().UTC())
}

// binaryOf is the process name a harness's sessions run under. Falling back to
// the registry key keeps a session recorded by a newer binary checkable.
func binaryOf(h session.Harness) string {
	if spec, ok := harness.For(h); ok {
		return spec.Binary
	}
	return string(h)
}
