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

func TestAccessibleJoinsWhenAutoJoinEnabled(t *testing.T) {
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

// CREW_AUTO_JOIN=0 is a delegated role's spawn saying it is deliberately not
// a room member; Accessible must not silently join it anyway.
func TestAccessibleDoesNotJoinWhenAutoJoinDisabled(t *testing.T) {
	t.Setenv("CREW_AUTO_JOIN", "0")
	st := seeded(t, agent(session.HarnessCodex, "aaa", 0))
	c := contextFor(st)

	keys, err := c.Accessible(context.Background(), "codex:aaa")
	if err != nil {
		t.Fatalf("Accessible: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("Accessible() = %v, want none: a delegated role must not be auto-joined", keys)
	}
}

// Only the join itself is skipped; membership recorded some other way (the
// ordinary session-start path, say) still reports normally.
func TestAccessibleStillReportsExistingMembership(t *testing.T) {
	st := seeded(t, agent(session.HarnessCodex, "aaa", 0))
	c := contextFor(st)
	if _, err := c.Accessible(context.Background(), "codex:aaa"); err != nil {
		t.Fatalf("Accessible: %v", err)
	}

	t.Setenv("CREW_AUTO_JOIN", "0")
	keys, err := c.Accessible(context.Background(), "codex:aaa")
	if err != nil {
		t.Fatalf("Accessible: %v", err)
	}
	if len(keys) != 1 || keys[0] != testRoom {
		t.Errorf("Accessible() = %v, want the membership already recorded", keys)
	}
}
