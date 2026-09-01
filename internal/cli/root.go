// Package cli implements the multiplayer command line.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/httpstore"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// Environment variables that supply defaults for the global flags.
const (
	envDB       = "MULTIPLAYER_DB"
	envServer   = "MULTIPLAYER_SERVER"
	envDebug    = "MULTIPLAYER_DEBUG"
	envAutoJoin = "MULTIPLAYER_AUTO_JOIN"
)

// options holds the global flags that decide which store the command talks to.
type options struct {
	db     string
	server string
}

// openStore returns the store the flags select: a registry server when one is
// configured, otherwise the local SQLite database. Both satisfy store.Store, so
// no command needs to know which it got.
func (o *options) openStore() (store.Store, error) {
	if o.server != "" {
		return httpstore.New(o.server), nil
	}
	path, err := o.dbPath()
	if err != nil {
		return nil, err
	}
	return sqlitestore.Open(path)
}

// dbPath resolves the database location, defaulting to ~/.multiplayer/sessions.db.
func (o *options) dbPath() (string, error) {
	if o.db != "" {
		return o.db, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".multiplayer", "sessions.db"), nil
}

// storeDir is the directory holding the database and hook log, which is what a
// sandboxed harness needs write access to.
func (o *options) storeDir() (string, error) {
	path, err := o.dbPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

func New() *cobra.Command {
	opts := &options{
		db:     os.Getenv(envDB),
		server: os.Getenv(envServer),
	}

	root := &cobra.Command{
		Use:   "multiplayer",
		Short: "Track which coding agents are working in which repos and worktrees",
		Long: `multiplayer is a registry of running coding-agent sessions.

Claude Code and Codex call "multiplayer hook" from their lifecycle hooks; each
call records the git checkout, and the pool slot if there is one.`,
		Version:       Version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&opts.db, "db", opts.db,
		"SQLite database path (default ~/.multiplayer/sessions.db) [$"+envDB+"]")
	root.PersistentFlags().StringVar(&opts.server, "server", opts.server,
		"registry server URL, used instead of the local database [$"+envServer+"]")
	root.CompletionOptions.HiddenDefaultCmd = true

	root.AddCommand(
		newHookCmd(opts),
		newListCmd(opts),
		newServeCmd(opts),
		newFleetCmd(opts),
		newPostCmd(opts),
		newResolveCmd(opts),
		newRoomCmd(opts),
		newSearchCmd(opts),
		newPromoteCmd(opts),
		newStateCmd(opts),
		newPruneCmd(opts),
		newInstallCmd(opts),
		newUninstallCmd(opts),
	)
	return root
}

func Execute() int {
	if err := New().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "multiplayer:", err)
		return 1
	}
	return 0
}
