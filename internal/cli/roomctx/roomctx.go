// Package roomctx resolves where a command is running into rooms, and which
// registered session is running it. Every room command needs both.
package roomctx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// BriefingLimit caps how much reference material a briefing carries per room.
const BriefingLimit = 12

// Context is everything a room command needs: where we are, and which store
// holds the entries.
type Context struct {
	Store store.Store
	Rooms store.RoomStore
	Here  []room.Room

	// close releases the store this context opened; nil when a caller
	// assembled the Context around a store it already owns.
	close func()
}

// Open resolves the current location into rooms. Room commands need the local
// database: the HTTP store does not carry rooms yet.
func Open(ctx context.Context, opts *cmdutil.Options, cwd string) (*Context, error) {
	if opts.Server != "" {
		return nil, errors.New("room commands need the local database; unset --server")
	}
	st, err := opts.OpenStore()
	if err != nil {
		return nil, err
	}
	rooms, ok := st.(store.RoomStore)
	if !ok {
		st.Close()
		return nil, errors.New("this store does not support rooms")
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
		return nil, errors.New("no room here")
	}
	return &Context{Store: st, Rooms: rooms, Here: here, close: func() { st.Close() }}, nil
}

// Close releases the store, if this context opened one.
func (c *Context) Close() {
	if c.close != nil {
		c.close()
	}
}

// Keys names every room this location belongs to.
func (c *Context) Keys() []string { return room.Keys(c.Here) }

// Has reports whether a room key is one of the rooms we are in.
func (c *Context) Has(key string) bool { return slices.Contains(c.Keys(), key) }

// Contains reports whether a session is working in one of these rooms.
func (c *Context) Contains(s *session.Session) bool { return inRooms(s, c.Keys()) }

// Target picks the worktree room, or the repository room when asked.
func (c *Context) Target(toRepo bool) room.Room {
	if !toRepo {
		return c.Here[0]
	}
	for _, r := range c.Here {
		if r.Scope == room.ScopeRepo {
			return r
		}
	}
	// A primary checkout is one place, so the two rooms are the same.
	target := c.Here[0]
	target.Scope = room.ScopeRepo
	return target
}

// Author names the session posting this, taking an explicit --as over inference.
func (c *Context) Author(ctx context.Context, as string) (string, error) {
	if as != "" {
		return as, nil
	}
	return ResolveAuthor(ctx, c.Store, c.Keys())
}

// Authors maps session keys to the friendly names entries are rendered with.
func (c *Context) Authors(ctx context.Context) (room.Authors, error) {
	sessions, err := c.Store.List(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	authors := room.Authors{}
	for _, s := range sessions {
		if s.Alias != "" {
			authors[s.Key()] = s.Alias
		}
	}
	return authors, nil
}

// Entry looks up one entry by id.
func (c *Context) Entry(ctx context.Context, id int64) (*room.Entry, error) {
	found, err := c.Rooms.Entries(ctx, room.Filter{IDs: []int64{id}})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no entry [%d]", id)
	}
	return found[0], nil
}

// Others names the active sessions in these rooms, excluding self.
func (c *Context) Others(ctx context.Context, self string) ([]string, error) {
	active, err := c.Store.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return nil, err
	}
	keys := c.Keys()
	var out []string
	for _, s := range active {
		if s.Key() == self || !inRooms(s, keys) {
			continue
		}
		out = append(out, fmt.Sprintf("%s (%s)", room.Display(s), room.Ago(s.LastSeen)))
	}
	return out, nil
}

// Join adds a session to every room for its location.
func Join(ctx context.Context, rs store.RoomStore, sessionKey string, rooms []room.Room) error {
	at := time.Now().UTC()
	for _, r := range rooms {
		m := &room.Membership{SessionKey: sessionKey, Room: r.Key, Scope: r.Scope, JoinedAt: at}
		if err := rs.Join(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// Cwd is where the command was run, which is what decides the room.
func Cwd() string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}

func inRooms(s *session.Session, keys []string) bool {
	for _, key := range room.Keys(room.For(s.Repo, s.Pool, s.CWD)) {
		if slices.Contains(keys, key) {
			return true
		}
	}
	return false
}
