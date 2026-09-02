package cli

import (
	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/harness/claude"
)

func newChannelCmd(_ *options) *cobra.Command {
	return &cobra.Command{
		Use:    "channel",
		Short:  "Push room entries into this Claude Code session",
		Hidden: true,
		Long: `Runs as an MCP server over stdio, spawned by Claude Code. It listens on a
per-session socket so the broker can push entries in without spawning a
process. Does nothing unless the session named it in --channels.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return claude.Serve(cmd.Context(), Version(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
