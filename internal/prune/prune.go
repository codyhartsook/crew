// Package prune ends sessions whose agent process is gone.
//
// A harness that is killed, or whose terminal closes, never fires SessionEnd,
// so its session would stay active forever. Only sessions on this machine can
// be checked.
package prune

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/proc"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// deadReason is recorded against sessions closed by pruning rather than by the
// harness, so the two are distinguishable after the fact.
const deadReason = "process gone"

// graceperiod protects a session that just started or just worked: a pid
// recorded moments ago that already looks wrong was more likely misread than
// orphaned, and reaping a live session is worse than listing a dead one.
const graceperiod = 60 * time.Second

// snapshot reads the process table, injected so tests need not shell out.
var snapshot = proc.Snapshot

// Dead lists active sessions on this host whose process is no longer there.
func Dead(ctx context.Context, st store.Store) ([]*session.Session, error) {
	active, err := st.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return nil, err
	}
	if len(active) == 0 {
		return nil, nil
	}
	// Without a process list nothing can be judged dead, and guessing would end
	// sessions that are running perfectly well. A sandbox that blocks process
	// inspection lands here, and must read as "cannot tell", not "nothing runs".
	table, err := snapshot()
	if err != nil {
		return nil, fmt.Errorf("read process table: %w", err)
	}
	if len(table) == 0 {
		return nil, errors.New("process table is empty; cannot tell which sessions are alive")
	}
	return deadAmong(active, table, detect.Hostname(), time.Now().UTC()), nil
}

// deadAmong decides which sessions to reap. Kept separate from reading the
// process table so the policy can be tested without one.
func deadAmong(active []*session.Session, table proc.Table, host string, now time.Time) []*session.Session {
	var dead []*session.Session
	for _, s := range active {
		// A pid means nothing on another machine, and an unrecorded pid cannot
		// be checked at all.
		if s.PID == 0 || (s.Host != "" && s.Host != host) {
			continue
		}
		if now.Sub(s.LastSeen) < graceperiod {
			continue
		}
		if !table.Running(s.PID, binaryOf(s.Harness)) {
			dead = append(dead, s)
		}
	}
	return dead
}

// End closes every session in dead, reporting how many were ended.
func End(ctx context.Context, st store.Store, dead []*session.Session, at time.Time) (int, error) {
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
