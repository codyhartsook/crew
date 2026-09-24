package role

// Role memory is keyed by room and role rather than session, so it survives
// the process that wrote it. It is a separate table from room entries, so it
// cannot leak through a missed filter.

import "time"

// MemoryEntry is one append-only write to a role's memory.
type MemoryEntry struct {
	ID   int64  `json:"id"`
	Room string `json:"room"`
	Role string `json:"role"`
	// Author is provenance only; memory is keyed on Room and Role.
	Author    string    `json:"author,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// MemoryFilter narrows a read. Zero-valued fields do not constrain the result.
type MemoryFilter struct {
	Room  string
	Role  string
	Limit int
}

// sessionPrefix keeps a session-derived key from ever colliding with a role.
const sessionPrefix = "session:"

// MemoryKey picks the memory identity: role if given, else the running session.
func MemoryKey(role, sessionKey string) string {
	if role != "" {
		return role
	}
	return sessionPrefix + sessionKey
}
