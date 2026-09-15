package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/rolemem"
)

// MemoryStore persists role memory, separate from RoomStore because it is a
// different table with a different audience: one role, not everyone in the
// room.
type MemoryStore interface {
	// WriteMemory appends an entry and assigns its ID.
	WriteMemory(ctx context.Context, e *rolemem.Entry) error

	// ReadMemory lists entries matching f, oldest first.
	ReadMemory(ctx context.Context, f rolemem.Filter) ([]*rolemem.Entry, error)
}
