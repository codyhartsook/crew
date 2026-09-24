package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/role"
)

// MemoryStore persists role memory: one role's audience, not the room's.
type MemoryStore interface {
	// WriteMemory appends an entry and assigns its ID.
	WriteMemory(ctx context.Context, e *role.MemoryEntry) error

	// ReadMemory lists entries matching f, oldest first.
	ReadMemory(ctx context.Context, f role.MemoryFilter) ([]*role.MemoryEntry, error)
}
