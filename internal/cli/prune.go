package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/detect"
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

func newPruneCmd(opts *options) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "End sessions whose agent process is gone",
		Long: `A harness that is killed, or whose terminal closes, never fires
SessionEnd, so its session would stay active forever. Only sessions on this
machine can be checked. Also runs automatically when a session starts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := opts.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			dead, err := findDead(cmd.Context(), st)
			if err != nil {
				return err
			}
			if len(dead) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no dead sessions")
				return nil
			}
			for _, s := range dead {
				verb := "ended"
				if dryRun {
					verb = "would end"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s (pid %d)\n", verb, s.Key(), s.PID)
			}
			if dryRun {
				return nil
			}
			_, err = endAll(cmd.Context(), st, dead, time.Now().UTC())
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be ended")
	return cmd
}

// findDead lists active sessions on this host whose process is no longer there.
func findDead(ctx context.Context, st store.Store) ([]*session.Session, error) {
	active, err := st.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return nil, err
	}
	if len(active) == 0 {
		return nil, nil
	}
	table, err := proc.Snapshot()
	if err != nil || len(table) == 0 {
		// Without a process list nothing can be judged dead, and guessing would
		// end sessions that are running perfectly well.
		return nil, fmt.Errorf("read process table: %w", err)
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
		if !table.Running(s.PID, string(s.Harness)) {
			dead = append(dead, s)
		}
	}
	return dead
}

func endAll(ctx context.Context, st store.Store, dead []*session.Session, at time.Time) (int, error) {
	n := 0
	for _, s := range dead {
		if err := st.End(ctx, s.Key(), at, deadReason); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// pruneQuietly reaps dead sessions without reporting. Used on the start path,
// where a failure to prune must never hold up a session.
func pruneQuietly(ctx context.Context, st store.Store) {
	dead, err := findDead(ctx, st)
	if err != nil || len(dead) == 0 {
		return
	}
	_, _ = endAll(ctx, st, dead, time.Now().UTC())
}
