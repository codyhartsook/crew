package roomcmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func NewPost(opts *cmdutil.Options) *cobra.Command {
	var (
		toRepo   bool
		resolves int64
		as       string
		to       string
	)

	cmd := &cobra.Command{
		Use:   "post <" + strings.Join(kindNames(), "|") + "> <body>",
		Short: "Post an entry to this worktree's room",
		Long: `Decisions and findings are reference material, read by agents arriving
in the room. Questions, handoffs and reviews are addressed to the other agents
here and stay open until resolved.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := room.Kind(args[0])
			if !kind.Valid() {
				return fmt.Errorf("unknown kind %q: want one of %s", args[0], strings.Join(kindNames(), ", "))
			}
			return post(cmd, opts, kind, strings.Join(args[1:], " "), toRepo, resolves, as, to)
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "post to the repository room")
	cmd.Flags().Int64Var(&resolves, "resolves", 0, "entry id this one answers or closes")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	cmd.Flags().StringVar(&to, "to", "", "send an addressed entry to this agent alias")
	return cmd
}

func post(cmd *cobra.Command, opts *cmdutil.Options, kind room.Kind, body string, toRepo bool, resolves int64, as, to string) error {
	rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
	if err != nil {
		return err
	}
	defer rc.Close()

	target := rc.Here[0]
	if toRepo {
		found := false
		for _, r := range rc.Here {
			if r.Scope == room.ScopeRepo {
				target, found = r, true
			}
		}
		// In a primary checkout the worktree and the repository are one room.
		if !found && rc.Here[0].Scope != room.ScopeRepo {
			target.Scope = room.ScopeRepo
		}
	}

	author, err := rc.Author(cmd.Context(), as)
	if err != nil {
		return err
	}
	recipient := ""
	if to != "" {
		if !kind.Addressed() {
			return errors.New("--to is only valid for questions, handoffs, and reviews")
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
		Kind:      kind,
		Author:    author,
		To:        recipient,
		Body:      body,
		Resolves:  resolves,
		CreatedAt: time.Now().UTC(),
	}
	if err := rc.Rooms.Post(cmd.Context(), e); err != nil {
		return err
	}
	if e.Kind.Addressed() {
		opts.SignalBroker()
	}
	if to != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "[%d] %s for %s posted to %s\n", e.ID, kind, to, target.Name)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "[%d] %s posted to %s\n", e.ID, kind, target.Name)
	}
	return nil
}

func kindNames() []string {
	out := make([]string, 0, len(room.Kinds))
	for _, k := range room.Kinds {
		out = append(out, string(k))
	}
	return out
}
