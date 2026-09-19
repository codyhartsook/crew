// Package rolememcmd reads or writes a role's private memory.
package rolememcmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/rolemem"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		role   string
		toRepo bool
		as     string
		asJSON bool
		last   int
	)

	cmd := &cobra.Command{
		Use:   "memory [body]",
		Short: "Read or write this role's private memory",
		Long: `With no body, prints what is stored for this role in this room. With a
body, appends to it.

Memory is keyed by room and role, not by session: it survives the process
that wrote it, and every spawn of a role shares one stream rather than one
each. It is private - never posted to the room and never in a generated
snapshot - and does not require a defined role: a caller with no role
identity falls back to its own session, so memory is usable before
delegation exists.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if last < 0 {
				return fmt.Errorf("--last must not be negative")
			}
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			if rc.Memory == nil {
				return errors.New("this store does not support role memory")
			}

			target, err := rc.Target(toRepo)
			if err != nil {
				return err
			}
			author, err := rc.Author(cmd.Context(), as)
			if err != nil {
				return err
			}

			memRole := rolemem.Key(roleIdentity(role), author)
			if len(args) == 0 {
				return read(cmd, rc, target.Key, memRole, asJSON, last)
			}
			return write(cmd, rc, target.Key, memRole, author, strings.Join(args, " "))
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "role identity to key memory on [$"+cmdutil.EnvRole+"]")
	cmd.Flags().BoolVar(&toRepo, "repo", false, "target the repository room")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	_ = cmd.Flags().MarkHidden("as")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a plain listing")
	cmd.Flags().IntVar(&last, "last", 0, "show only the newest N entries")
	return cmd
}

// roleIdentity prefers an explicit --role over the spawn's own environment.
func roleIdentity(role string) string {
	if role != "" {
		return role
	}
	return os.Getenv(cmdutil.EnvRole)
}

func write(cmd *cobra.Command, rc *roomctx.Context, roomKey, memRole, author, body string) error {
	e := &rolemem.Entry{Room: roomKey, Role: memRole, Author: author, Body: body, CreatedAt: time.Now().UTC()}
	if err := rc.Memory.WriteMemory(cmd.Context(), e); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[%d] written to %s memory\n", e.ID, memRole)
	return nil
}

func read(cmd *cobra.Command, rc *roomctx.Context, roomKey, memRole string, asJSON bool, last int) error {
	entries, err := rc.Memory.ReadMemory(cmd.Context(), rolemem.Filter{Room: roomKey, Role: memRole, Limit: last})
	if err != nil {
		return err
	}
	if asJSON {
		return cmdutil.WriteJSON(cmd.OutOrStdout(), entries)
	}
	if len(entries) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: no memory yet\n", memRole)
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(cmd.OutOrStdout(), "[%d] %s\n%s\n\n", e.ID, e.CreatedAt.Format(time.RFC3339), e.Body)
	}
	return nil
}
