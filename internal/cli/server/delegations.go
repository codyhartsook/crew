package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// delegationPollInterval is how often the broker looks for queued work.
const delegationPollInterval = 2 * time.Second

// delegationTimeout bounds one spawn, matching the CLI's --wait default.
const delegationTimeout = 10 * time.Minute

// staleRunningTimeout is how long "running" may go unclaimed before it is
// assumed orphaned by a crashed process and failed. Must exceed delegationTimeout.
const staleRunningTimeout = 2 * delegationTimeout

// delegationCoordinator runs fire-and-forget delegations from any repo.
type delegationCoordinator struct {
	store store.DelegationStore
	// chans is the same store when it carries the back-channel.
	chans store.ChannelStore
	log   *slog.Logger

	mu      sync.Mutex
	running map[string]bool
	workers sync.WaitGroup
}

func newDelegationCoordinator(st store.DelegationStore, log *slog.Logger) *delegationCoordinator {
	c := &delegationCoordinator{store: st, log: log, running: map[string]bool{}}
	c.chans, _ = st.(store.ChannelStore)
	return c
}

func (c *delegationCoordinator) run(ctx context.Context) {
	t := time.NewTicker(delegationPollInterval)
	defer t.Stop()
	for {
		c.sweep(ctx)
		select {
		case <-ctx.Done():
			c.workers.Wait()
			return
		case <-t.C:
		}
	}
}

// sweep claims and spawns pending delegations concurrently.
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
		c.workers.Add(1)
		go func() {
			defer c.workers.Done()
			c.execute(ctx, d)
		}()
	}
	c.reapStale(ctx)
}

// reapStale fails a "running" delegation this process isn't executing and
// hasn't touched in staleRunningTimeout.
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
			continue
		}
		c.closeAsks(ctx, d)
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

	def, err := delegation.ResolveIn(ctx, d.Dir, d.Role)
	if err != nil {
		c.fail(ctx, d, err)
		return
	}
	env := []string{
		cmdutil.EnvAutoJoin + "=0",
		cmdutil.EnvRole + "=" + d.Role,
		cmdutil.EnvDelegation + "=" + d.ID,
	}
	spawnCtx, cancel := context.WithTimeout(ctx, delegationTimeout)
	defer cancel()

	result, err := delegation.Run(spawnCtx, session.Harness(d.Harness), def, d.Prompt, d.Dir, env)
	if err != nil {
		c.fail(ctx, d, err)
		return
	}
	if err := c.store.CompleteDelegation(ctx, d.ID, result); err != nil {
		c.log.Warn("complete delegation", "id", d.ID, "err", err)
	}
	c.closeAsks(ctx, d)
}

func (c *delegationCoordinator) closeAsks(ctx context.Context, d *delegation.Delegation) {
	if err := store.CloseAsks(ctx, c.store, c.chans, d.ID); err != nil {
		c.log.Warn("close delegation questions", "id", d.ID, "err", err)
	}
}

func (c *delegationCoordinator) fail(ctx context.Context, d *delegation.Delegation, cause error) {
	if err := c.store.FailDelegation(ctx, d.ID, cause.Error()); err != nil {
		c.log.Warn("fail delegation", "id", d.ID, "err", err)
	}
	c.closeAsks(ctx, d)
}
