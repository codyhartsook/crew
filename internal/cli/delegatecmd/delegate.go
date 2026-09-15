// Package delegatecmd spawns a role headless, or queues it for the broker.
package delegatecmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/delegate"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// defaultTimeout bounds a --wait spawn.
const defaultTimeout = 10 * time.Minute

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		wait        bool
		toRepo      bool
		as          string
		harnessFlag string
		timeout     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "delegate <role> <task>",
		Short: "Spawn a role to do one task and return its result",
		Long: `Fire-and-forget by default: queues the task and returns a delegation id
immediately, and its result surfaces on your next notice once the broker
finishes it. --wait blocks here instead and prints the result directly.

Either way, the role spawns headless with room auto-join disabled.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			roleName, task := args[0], strings.Join(args[1:], " ")

			def, err := delegate.Resolve(cmd.Context(), roleName)
			if err != nil {
				return err
			}
			h, err := delegate.PickHarness(def, harnessFlag)
			if err != nil {
				return err
			}

			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			ds, ok := rc.Store.(store.DelegationStore)
			if !ok {
				return errors.New("this store does not support delegation")
			}
			target, err := rc.Target(toRepo)
			if err != nil {
				return err
			}
			author, err := rc.Author(cmd.Context(), as)
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve working directory: %w", err)
			}
			now := time.Now().UTC()
			d := &delegation.Delegation{
				ID: uuid.NewString(), Room: target.Key, Dir: cwd, Role: def.Name, Harness: string(h),
				Requester: author, Prompt: task, Status: delegation.StatusPending,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := ds.CreateDelegation(cmd.Context(), d); err != nil {
				return err
			}

			if !wait {
				fmt.Fprintf(cmd.OutOrStdout(), "delegation %s queued for %s (%s)\n", d.ID, def.Name, h)
				return nil
			}
			return runNow(cmd, ds, d, def, h, timeout)
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", false, "block here and print the result, instead of queueing it")
	cmd.Flags().BoolVar(&toRepo, "repo", false, "delegate for the repository room")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	_ = cmd.Flags().MarkHidden("as")
	cmd.Flags().StringVar(&harnessFlag, "harness", "", "which harness runs the role, when its definition allows either")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultTimeout, "how long to wait before killing the spawn")
	cmd.AddCommand(newResult(opts))
	return cmd
}

// runNow runs d synchronously for --wait; nothing is left to notify after.
func runNow(cmd *cobra.Command, ds store.DelegationStore, d *delegation.Delegation, def role.Definition, h session.Harness, timeout time.Duration) error {
	ctx := cmd.Context()
	if started, err := ds.StartDelegation(ctx, d.ID); err != nil || !started {
		if err == nil {
			err = errors.New("delegation was already claimed")
		}
		return err
	}

	env := []string{
		cmdutil.EnvAutoJoin + "=0",
		cmdutil.EnvRole + "=" + def.Name,
		cmdutil.EnvDelegation + "=" + d.ID,
	}
	spawnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := delegate.Run(spawnCtx, h, def, d.Prompt, d.Dir, env)
	_ = ds.MarkNotified(ctx, d.ID)
	if err != nil {
		_ = ds.FailDelegation(ctx, d.ID, err.Error())
		return err
	}
	if err := ds.CompleteDelegation(ctx, d.ID, result); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), result)
	return nil
}
