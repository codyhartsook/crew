package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// briefingLimit caps how much reference material a briefing carries.
const briefingLimit = 12

// roomContext is everything a room command needs: where we are, and which
// store holds the entries.
type roomContext struct {
	store store.Store
	rooms store.RoomStore
	here  []room.Room
}

// openRoomContext resolves the current location into rooms. Room commands need
// the local database: the HTTP store does not carry rooms yet.
func openRoomContext(ctx context.Context, opts *options, cwd string) (*roomContext, func(), error) {
	if opts.server != "" {
		return nil, nil, errors.New("room commands need the local database; unset --server")
	}
	st, err := opts.openStore()
	if err != nil {
		return nil, nil, err
	}
	rooms, ok := st.(store.RoomStore)
	if !ok {
		st.Close()
		return nil, nil, errors.New("this store does not support rooms")
	}

	loc, _ := detect.New().Detect(ctx, cwd)
	var (
		repo *session.Repo
		pool *session.Pool
	)
	resolved := cwd
	if loc != nil {
		repo, pool = loc.Repo, loc.Pool
		if loc.CWD != "" {
			resolved = loc.CWD
		}
	}
	here := room.For(repo, pool, resolved)
	if len(here) == 0 {
		st.Close()
		return nil, nil, errors.New("no room here")
	}
	return &roomContext{store: st, rooms: rooms, here: here}, func() { st.Close() }, nil
}

func newPostCmd(opts *options) *cobra.Command {
	var (
		toRepo   bool
		resolves int64
		as       string
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
			return post(cmd, opts, kind, strings.Join(args[1:], " "), toRepo, resolves, as)
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "post to the repository room")
	cmd.Flags().Int64Var(&resolves, "resolves", 0, "entry id this one answers or closes")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	return cmd
}

func newResolveCmd(opts *options) *cobra.Command {
	var as string

	cmd := &cobra.Command{
		Use:   "resolve <id> <body>",
		Short: "Answer or close an open entry",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid entry id %q", args[0])
			}
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			target, err := entryByID(cmd.Context(), rc.rooms, id)
			if err != nil {
				return err
			}
			author, err := authorFor(cmd.Context(), rc, as)
			if err != nil {
				return err
			}

			// A resolution inherits the room and kind of what it closes so the
			// pair reads as one thread.
			e := &room.Entry{
				Room:      target.Room,
				Scope:     target.Scope,
				Kind:      target.Kind,
				Author:    author,
				Body:      strings.Join(args[1:], " "),
				Resolves:  target.ID,
				CreatedAt: time.Now().UTC(),
			}
			if err := rc.rooms.Post(cmd.Context(), e); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "resolved [%d] with [%d]\n", target.ID, e.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	return cmd
}

func newRemoveCmd(opts *options) *cobra.Command {
	var as string

	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove one of your unthreaded entries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("invalid entry id %q", args[0])
			}
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			target, err := entryByID(cmd.Context(), rc.rooms, id)
			if err != nil {
				return err
			}
			if !contains(room.Keys(rc.here), target.Room) {
				return fmt.Errorf("entry [%d] is not in this room", id)
			}
			author, err := authorFor(cmd.Context(), rc, as)
			if err != nil {
				return err
			}
			removed, err := rc.rooms.RemoveEntry(cmd.Context(), id, author)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("entry [%d] is not yours or is part of a thread", id)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed [%d]\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	return cmd
}

func post(cmd *cobra.Command, opts *options, kind room.Kind, body string, toRepo bool, resolves int64, as string) error {
	rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
	if err != nil {
		return err
	}
	defer closeFn()

	target := rc.here[0]
	if toRepo {
		found := false
		for _, r := range rc.here {
			if r.Scope == room.ScopeRepo {
				target, found = r, true
			}
		}
		// In a primary checkout the worktree and the repository are one room.
		if !found && rc.here[0].Scope != room.ScopeRepo {
			target.Scope = room.ScopeRepo
		}
	}

	author, err := authorFor(cmd.Context(), rc, as)
	if err != nil {
		return err
	}

	e := &room.Entry{
		Room:      target.Key,
		Scope:     target.Scope,
		Kind:      kind,
		Author:    author,
		Body:      body,
		Resolves:  resolves,
		CreatedAt: time.Now().UTC(),
	}
	if err := rc.rooms.Post(cmd.Context(), e); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[%d] %s posted to %s\n", e.ID, kind, target.Name)
	return nil
}

func newRoomCmd(opts *options) *cobra.Command {
	var (
		asJSON bool
		inbox  bool
		ack    bool
	)

	cmd := &cobra.Command{
		Use:     "room",
		Aliases: []string{"rooms"},
		Short:   "Show the shared context for where you are",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if ack && !inbox {
				return errors.New("--ack requires --inbox")
			}
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()
			if inbox {
				author, err := authorFor(cmd.Context(), rc, "")
				if err != nil {
					return err
				}
				entries, err := rc.rooms.Unread(cmd.Context(), author)
				if err != nil {
					return err
				}
				if len(entries) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "nothing new")
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), room.Delivery(entries))
				if ack {
					return rc.rooms.Ack(cmd.Context(), author, entries[len(entries)-1].ID)
				}
				return nil
			}

			entries, err := rc.rooms.Entries(cmd.Context(), room.Filter{
				Rooms: room.Keys(rc.here),
				Limit: briefingLimit * len(rc.here) * 3,
			})
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			// Listing the caller as "also here" is noise; failing to identify
			// it is not a reason to refuse the briefing.
			self, _ := authorFor(cmd.Context(), rc, "")
			others, err := otherAgents(cmd.Context(), rc, self)
			if err != nil {
				return err
			}
			values, err := rc.rooms.States(cmd.Context(), room.StateFilter{Rooms: room.Keys(rc.here)})
			if err != nil {
				return err
			}
			out := room.Briefing(rc.here, entries, values, others)
			if out == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: nothing posted yet\n", rc.here[0].Name)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a briefing")
	cmd.Flags().BoolVar(&inbox, "inbox", false, "show unread addressed entries")
	cmd.Flags().BoolVar(&ack, "ack", false, "mark inbox entries as delivered")
	return cmd
}

// JoinRooms adds a session to every room for its location.
func JoinRooms(ctx context.Context, rs store.RoomStore, sessionKey string, rooms []room.Room) error {
	now := time.Now().UTC()
	for _, r := range rooms {
		m := &room.Membership{SessionKey: sessionKey, Room: r.Key, Scope: r.Scope, JoinedAt: now}
		if err := rs.Join(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// otherAgents names the active sessions in these rooms, excluding self.
func otherAgents(ctx context.Context, rc *roomContext, self string) ([]string, error) {
	active, err := rc.store.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return nil, err
	}
	keys := room.Keys(rc.here)
	var out []string
	for _, s := range active {
		if s.Key() == self || s.Repo == nil || !contains(keys, s.Repo.Root) {
			continue
		}
		out = append(out, fmt.Sprintf("%s (%s)", room.Author(s.Key()), room.Ago(s.LastSeen)))
	}
	return out, nil
}

func authorFor(ctx context.Context, rc *roomContext, as string) (string, error) {
	if as != "" {
		return as, nil
	}
	return resolveAuthor(ctx, rc.store, room.Keys(rc.here))
}

func entryByID(ctx context.Context, rs store.RoomStore, id int64) (*room.Entry, error) {
	found, err := rs.Entries(ctx, room.Filter{IDs: []int64{id}})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no entry [%d]", id)
	}
	return found[0], nil
}

// roomFor picks the worktree room, or the repository room when asked.
func roomFor(rc *roomContext, toRepo bool) room.Room {
	if !toRepo {
		return rc.here[0]
	}
	for _, r := range rc.here {
		if r.Scope == room.ScopeRepo {
			return r
		}
	}
	// A primary checkout is one place, so the two rooms are the same.
	target := rc.here[0]
	target.Scope = room.ScopeRepo
	return target
}

func kindNames() []string {
	out := make([]string, 0, len(room.Kinds))
	for _, k := range room.Kinds {
		out = append(out, string(k))
	}
	return out
}

func cwdOf(*cobra.Command) string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}
