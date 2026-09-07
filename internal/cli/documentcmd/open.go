package documentcmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/roomdoc"
)

func NewOpen(opts *cmdutil.Options) *cobra.Command {
	var (
		target targetFlags
		noOpen bool
	)
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open a generated view of this room",
		Long:  "Generates a read-only Markdown snapshot. A shell without an agent identity is treated as the local person.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			r, _, err := resolve(cmd.Context(), rc, isHuman(), target)
			if err != nil {
				return err
			}
			dir, err := documentDir(opts, r.Key)
			if err != nil {
				return err
			}
			path, err := roomdoc.Write(cmd.Context(), rc.Store, rc.Rooms, dir, r)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			if noOpen {
				return nil
			}
			program, args := opener(path)
			return exec.CommandContext(cmd.Context(), program, args...).Start()
		},
	}
	target.bind(cmd)
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "generate and print the path without opening it")
	return cmd
}
