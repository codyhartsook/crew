// Package documentcmd exposes a room's filesystem document store.
package documentcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/documents"
	"github.com/codyhartsook/multiplayer/internal/room"
)

type targetFlags struct {
	repo    bool
	roomKey string
}

func (f *targetFlags) bind(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.repo, "repo", false, "use the repository room")
	cmd.Flags().StringVar(&f.roomKey, "room", "", "exact room ID (local human shells only)")
}

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		target targetFlags
		path   bool
	)
	cmd := &cobra.Command{
		Use:     "docs",
		Aliases: []string{"documents"},
		Short:   "List, publish or open this room's documents",
		Long:    "Lists this room's documents. A shell without an agent identity is treated as the local person.",
		Args:    cobra.NoArgs,
		RunE:    listRun(opts, &target, &path),
	}
	target.bind(cmd)
	cmd.Flags().BoolVar(&path, "path", false, "print only the document directory")
	cmd.AddCommand(newLs(opts), newPublish(opts), newUnpublish(opts), newOpen(opts))
	return cmd
}

// listRun prints the room's documents, or only their directory with path.
func listRun(opts *cmdutil.Options, target *targetFlags, path *bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
		if err != nil {
			return err
		}
		defer rc.Close()
		r, _, err := resolve(cmd.Context(), rc, isHuman(), *target)
		if err != nil {
			return err
		}
		dir, err := documentDir(opts, r.Key)
		if err != nil {
			return err
		}
		if *path {
			fmt.Fprintln(cmd.OutOrStdout(), dir)
			return nil
		}
		docs, err := documents.List(dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)  %s\n", r.Name, r.Scope, dir)
		if len(docs) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No documents yet.")
			return nil
		}
		for _, doc := range docs {
			fmt.Fprintln(cmd.OutOrStdout(), doc.Name)
		}
		return nil
	}
}

func newLs(opts *cmdutil.Options) *cobra.Command {
	var target targetFlags
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List this room's documents",
		Args:    cobra.NoArgs,
		RunE:    listRun(opts, &target, new(bool)),
	}
	target.bind(cmd)
	return cmd
}

func newPublish(opts *cmdutil.Options) *cobra.Command {
	var target targetFlags
	cmd := &cobra.Command{
		Use:   "publish <file>",
		Short: "Publish a document to this room",
		Long:  "Copies and announces a document. A shell without an agent identity publishes as the local person.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			r, author, err := resolve(cmd.Context(), rc, isHuman(), target)
			if err != nil {
				return err
			}
			dir, err := documentDir(opts, r.Key)
			if err != nil {
				return err
			}
			name, err := documents.Publish(args[0], dir)
			if err != nil {
				return err
			}
			e := &room.Entry{
				Room: r.Key, Scope: r.Scope, Mode: room.ModeNote, Author: author,
				Body: documents.PublishedNote + name, CreatedAt: time.Now().UTC(),
			}
			if err := rc.Rooms.Post(cmd.Context(), e); err != nil {
				return fmt.Errorf("document was copied to %s but its room announcement failed: %w", filepath.Join(dir, name), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Published %s to %s (%s)\n", name, r.Name, r.Scope)
			return nil
		},
	}
	target.bind(cmd)
	return cmd
}

func resolve(ctx context.Context, rc *roomctx.Context, human bool, flags targetFlags) (room.Room, string, error) {
	if flags.repo && flags.roomKey != "" {
		return room.Room{}, "", errors.New("--repo and --room cannot be used together")
	}
	var (
		target room.Room
		err    error
	)
	if flags.roomKey != "" {
		if !human {
			return room.Room{}, "", errors.New("--room is only available to local human commands; agents may only use their current rooms")
		}
		target, err = findRoom(ctx, rc, flags.roomKey)
		if err != nil {
			return room.Room{}, "", err
		}
	} else if target, err = rc.Target(flags.repo); err != nil {
		return room.Room{}, "", err
	}

	if human {
		return target, room.HumanAuthor(), nil
	}
	author, err := rc.Author(ctx, "")
	if err != nil {
		return room.Room{}, "", err
	}
	keys, err := rc.Accessible(ctx, author)
	if err != nil {
		return room.Room{}, "", err
	}
	if !slices.Contains(keys, target.Key) {
		return room.Room{}, "", errors.New("the active agent has not joined this room")
	}
	return target, author, nil
}

func findRoom(ctx context.Context, rc *roomctx.Context, key string) (room.Room, error) {
	for _, candidate := range rc.Here {
		if candidate.Key == key {
			return candidate, nil
		}
	}
	if members, err := rc.Rooms.Members(ctx, key); err != nil {
		return room.Room{}, err
	} else if len(members) > 0 {
		return room.Room{Key: key, Scope: members[0].Scope, Name: room.NameFor(key)}, nil
	}
	entries, err := rc.Rooms.Entries(ctx, room.Filter{Rooms: []string{key}, Limit: 1})
	if err != nil {
		return room.Room{}, err
	}
	if len(entries) == 0 {
		return room.Room{}, fmt.Errorf("room %q does not exist", key)
	}
	return room.Room{Key: key, Scope: entries[0].Scope, Name: room.NameFor(key)}, nil
}

func isHuman() bool {
	return !roomctx.AgentEnvironment()
}

func documentDir(opts *cmdutil.Options, roomKey string) (string, error) {
	root, err := opts.StoreDir()
	if err != nil {
		return "", err
	}
	return documents.Dir(root, roomKey)
}

func opener(path string) (string, []string) {
	for _, variable := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(os.Getenv(variable)); len(fields) > 0 {
			return fields[0], append(fields[1:], path)
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}
	default:
		return "xdg-open", []string{path}
	}
}
