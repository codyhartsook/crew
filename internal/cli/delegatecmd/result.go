package delegatecmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func newResult(opts *cmdutil.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "result <id>",
		Short: "Show a delegation's status and result",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			ds, ok := rc.Store.(store.DelegationStore)
			if !ok {
				return errors.New("this store does not support delegation")
			}
			d, err := ds.GetDelegation(cmd.Context(), id)
			if err != nil {
				return err
			}
			if !d.Notified {
				_ = ds.MarkNotified(cmd.Context(), id)
			}

			switch d.Status {
			case delegation.StatusPending:
				fmt.Fprintf(cmd.OutOrStdout(), "%s: queued, not started yet\n", id)
			case delegation.StatusRunning:
				fmt.Fprintf(cmd.OutOrStdout(), "%s: still running\n", id)
			case delegation.StatusFailed:
				return fmt.Errorf("%s: %s", id, d.Error)
			case delegation.StatusDone:
				fmt.Fprintln(cmd.OutOrStdout(), d.Result)
			default:
				return fmt.Errorf("%s: unknown status %q", id, d.Status)
			}
			return nil
		},
	}
	return cmd
}
