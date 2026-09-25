// Package cli assembles the crew command line. Small commands live here;
// larger ones have their own package.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/dashboardcmd"
	"github.com/codyhartsook/multiplayer/internal/cli/documentcmd"
	"github.com/codyhartsook/multiplayer/internal/cli/hookcmd"
	"github.com/codyhartsook/multiplayer/internal/cli/initcmd"
	"github.com/codyhartsook/multiplayer/internal/cli/lscmd"
	"github.com/codyhartsook/multiplayer/internal/cli/roomcmd"
	"github.com/codyhartsook/multiplayer/internal/cli/uninstallcmd"
	"github.com/codyhartsook/multiplayer/internal/version"
)

func New() *cobra.Command {
	opts := cmdutil.FromEnv()

	root := &cobra.Command{
		Use:   "crew",
		Short: "Track which coding agents are working in which repos and worktrees",
		Long: `crew is a registry of running coding-agent sessions.

Claude Code and Codex call "crew hook" from their lifecycle hooks; each
call records the git checkout, and the pool slot if there is one.`,
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&opts.DB, "db", opts.DB,
		"SQLite database path (default ~/.multiplayer/sessions.db) [$"+cmdutil.EnvDB+"]")
	root.PersistentFlags().StringVar(&opts.Server, "server", opts.Server,
		"registry server URL, used instead of the local database [$"+cmdutil.EnvServer+"]")
	root.PersistentFlags().BoolVar(&opts.Human, "human", false,
		"render for a person: more columns, less terse")
	root.CompletionOptions.HiddenDefaultCmd = true

	root.AddCommand(
		hookcmd.New(opts),
		lscmd.New(opts),
		newAnchor(opts),
		newServe(opts),
		dashboardcmd.New(opts),
		documentcmd.New(opts),
		roomcmd.NewPost(opts),
		roomcmd.NewResolve(opts),
		roomcmd.NewRemove(opts),
		newRole(opts),
		roomcmd.New(opts),
		newSearch(opts),
		newClear(opts),
		newPrune(opts),
		initcmd.New(opts),
		uninstallcmd.New(opts),
	)
	return root
}

func Execute() int {
	if err := New().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "crew:", err)
		return 1
	}
	return 0
}
