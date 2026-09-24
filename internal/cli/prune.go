package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/prune"
)

func newPrune(opts *cmdutil.Options) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:    "prune",
		Short:  "End sessions whose agent process is gone",
		Hidden: true,
		Long: `A harness that is killed, or whose terminal closes, never fires
SessionEnd, so its session would stay active forever. Only sessions on this
machine can be checked. Also runs automatically when a session starts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer st.Close()

			dead, err := prune.Dead(cmd.Context(), st)
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
			_, err = prune.End(cmd.Context(), st, dead, time.Now().UTC())
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be ended")
	return cmd
}
