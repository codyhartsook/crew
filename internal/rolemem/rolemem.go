// Package rolemem is a role's private memory, keyed by room and role rather
// than session so it survives the process that wrote it and is shared by
// concurrent instances of a role. A separate table from room entries, not a
// visibility flag, so it cannot leak by a missed filter.
package rolemem

import "time"

// Entry is one append-only write to a role's memory.
type Entry struct {
	ID   int64  `json:"id"`
	Room string `json:"room"`
	Role string `json:"role"`
	// Author is provenance only; memory is keyed on Room and Role.
	Author    string    `json:"author,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Filter narrows a read. Zero-valued fields do not constrain the result.
type Filter struct {
	Room  string
	Role  string
	Limit int
}

// sessionPrefix keeps a session-derived key from ever colliding with a role.
const sessionPrefix = "session:"

// Key picks the memory identity: role if given, else the running session.
func Key(role, sessionKey string) string {
	if role != "" {
		return role
	}
	return sessionPrefix + sessionKey
}
