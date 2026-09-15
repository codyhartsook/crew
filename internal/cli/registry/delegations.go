package registry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/delegate"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// delegationPollInterval is how often the broker looks for queued work.
const delegationPollInterval = 2 * time.Second

// delegationTimeout bounds one spawn, matching the CLI's --wait default.
const delegationTimeout = 10 * time.Minute

// staleRunningTimeout is how long a "running" delegation may sit untouched by
// this process's own claim before it is assumed orphaned - the broker (or a
// --wait launcher) that was running it crashed or restarted - and failed so
// a retry is possible. It must clear delegationTimeout: a spawn still
// legitimately in flight elsewhere updates nothing until it finishes.
const staleRunningTimeout = 2 * delegationTimeout

// delegationCoordinator runs fire-and-forget delegations. A pending row can
// come from any repo on the machine, since one broker serves them all.
type delegationCoordinator struct {
	store store.DelegationStore
	log   *slog.Logger

	mu      sync.Mutex
	running map[string]bool
}

func newDelegationCoordinator(st store.DelegationStore, log *slog.Logger) *delegationCoordinator {
	return &delegationCoordinator{store: st, log: log, running: map[string]bool{}}
}

func (c *delegationCoordinator) run(ctx context.Context) {
	t := time.NewTicker(delegationPollInterval)
	defer t.Stop()
	for {
		c.sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sweep claims and spawns pending delegations concurrently, so one long spawn
// never delays the next poll.
func (c *delegationCoordinator) sweep(ctx context.Context) {
	pending, err := c.store.ListDelegations(ctx, delegation.Filter{Status: delegation.StatusPending})
	if err != nil {
		c.log.Warn("delegation sweep", "err", err)
		return
	}
	for _, d := range pending {
		if !c.claim(d.ID) {
			continue
		}
		started, err := c.store.StartDelegation(ctx, d.ID)
		if err != nil || !started {
			c.release(d.ID)
			if err != nil {
				c.log.Warn("start delegation", "id", d.ID, "err", err)
			}
			continue
		}
		go c.execute(ctx, d)
	}
	c.reapStale(ctx)
}

// reapStale fails a "running" delegation this process is not itself
// executing and has not been touched in staleRunningTimeout: the broker that
// claimed it is gone, and nothing else will ever move it out of "running".
func (c *delegationCoordinator) reapStale(ctx context.Context) {
	running, err := c.store.ListDelegations(ctx, delegation.Filter{Status: delegation.StatusRunning})
	if err != nil {
		c.log.Warn("delegation stale sweep", "err", err)
		return
	}
	for _, d := range running {
		if c.isRunning(d.ID) || time.Since(d.UpdatedAt) < staleRunningTimeout {
			continue
		}
		if err := c.store.FailDelegation(ctx, d.ID, "orphaned: the process running this was gone for longer than it should ever take"); err != nil {
			c.log.Warn("reap stale delegation", "id", d.ID, "err", err)
		}
	}
}

func (c *delegationCoordinator) isRunning(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running[id]
}

func (c *delegationCoordinator) claim(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running[id] {
		return false
	}
	c.running[id] = true
	return true
}

func (c *delegationCoordinator) release(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.running, id)
}

func (c *delegationCoordinator) execute(ctx context.Context, d *delegation.Delegation) {
	defer c.release(d.ID)

	def, err := delegate.ResolveIn(ctx, d.Dir, d.Role)
	if err != nil {
		c.fail(ctx, d.ID, err)
		return
	}
	env := []string{
		cmdutil.EnvAutoJoin + "=0",
		cmdutil.EnvRole + "=" + d.Role,
		cmdutil.EnvDelegation + "=" + d.ID,
	}
	spawnCtx, cancel := context.WithTimeout(ctx, delegationTimeout)
	defer cancel()

	result, err := delegate.Run(spawnCtx, session.Harness(d.Harness), def, d.Prompt, d.Dir, env)
	if err != nil {
		c.fail(ctx, d.ID, err)
		return
	}
	if err := c.store.CompleteDelegation(ctx, d.ID, result); err != nil {
		c.log.Warn("complete delegation", "id", d.ID, "err", err)
	}
}

func (c *delegationCoordinator) fail(ctx context.Context, id string, cause error) {
	if err := c.store.FailDelegation(ctx, id, cause.Error()); err != nil {
		c.log.Warn("fail delegation", "id", id, "err", err)
	}
}
