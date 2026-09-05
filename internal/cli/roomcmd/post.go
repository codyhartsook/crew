package roomcmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func NewPost(opts *cmdutil.Options) *cobra.Command {
	var (
		toRepo bool
		as     string
		to     string
	)

	cmd := &cobra.Command{
		Use:   "post <" + strings.Join(modeNames(), "|") + "> <body>",
		Short: "Post an entry to this worktree's room",
		Long: `Notes are durable context for the room. Requests are delivered to other
agents and stay open until resolved.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := room.Mode(args[0])
			if !mode.Valid() {
				return fmt.Errorf("unknown mode %q: want one of %s", args[0], strings.Join(modeNames(), ", "))
			}
			return post(cmd, opts, mode, strings.Join(args[1:], " "), toRepo, as, to)
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "post to the repository room")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	_ = cmd.Flags().MarkHidden("as")
	cmd.Flags().StringVar(&to, "to", "", "send an addressed entry to this agent alias")
	return cmd
}

func post(cmd *cobra.Command, opts *cmdutil.Options, mode room.Mode, body string, toRepo bool, as, to string) error {
	rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
	if err != nil {
		return err
	}
	defer rc.Close()

	target, err := rc.Target(toRepo)
	if err != nil {
		return err
	}

	author, err := rc.Author(cmd.Context(), as)
	if err != nil {
		return err
	}
	keys, err := rc.Accessible(cmd.Context(), author)
	if err != nil {
		return err
	}
	if !slices.Contains(keys, target.Key) {
		return errors.New("the active agent has not joined this room")
	}
	recipient := ""
	if to != "" {
		if !mode.Addressed() {
			return errors.New("--to is only valid for requests")
		}
		recipient, err = roomctx.ResolveAgent(cmd.Context(), rc.Store, []string{target.Key}, to)
		if err != nil {
			return err
		}
		if recipient == author {
			return errors.New("cannot target yourself")
		}
	}

	e := &room.Entry{
		Room:      target.Key,
		Scope:     target.Scope,
		Mode:      mode,
		Author:    author,
		To:        recipient,
		Body:      body,
		CreatedAt: time.Now().UTC(),
	}
	if err := rc.Rooms.Post(cmd.Context(), e); err != nil {
		return err
	}
	if e.Mode.Addressed() {
		opts.SignalBroker()
	}
	if to != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "[%d] %s for %s posted to %s\n", e.ID, mode, to, target.Name)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "[%d] %s posted to %s\n", e.ID, mode, target.Name)
	}
	return nil
}

func modeNames() []string {
	out := make([]string, 0, len(room.Modes))
	for _, k := range room.Modes {
		out = append(out, string(k))
	}
	return out
}
