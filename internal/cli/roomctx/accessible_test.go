package roomctx

import (
	"context"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func contextFor(st store.Store) *Context {
	return &Context{Store: st, Rooms: st.(store.RoomStore), Here: []room.Room{
		{Key: testRoom, Scope: room.ScopeWorktree, Name: "widget"},
	}}
}

func TestAccessibleJoinsTheRoom(t *testing.T) {
	st := seeded(t, agent(session.HarnessCodex, "aaa", 0))
	c := contextFor(st)

	keys, err := c.Accessible(context.Background(), "codex:aaa")
	if err != nil {
		t.Fatalf("Accessible: %v", err)
	}
	if len(keys) != 1 || keys[0] != testRoom {
		t.Errorf("Accessible() = %v, want [%s]", keys, testRoom)
	}
}
