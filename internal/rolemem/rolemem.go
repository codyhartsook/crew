// Package rolemem is a role's private memory: what a spawned role writes for
// its next spawn to read. Entries are keyed by room and role, never by
// session, so a role's knowledge survives the process that produced it and
// concurrent instances of a role share one stream instead of one each.
//
// Memory is a separate table from room entries, not a visibility flag on one:
// a flag has to be filtered correctly by every read path, including a
// generated snapshot, and one missed filter leaks. A separate table cannot
// leak by omission.
package rolemem

import "time"

// Entry is one write to a role's memory. Entries are append-only: memory is a
// stream a role writes to across spawns, not a document overwritten in place.
type Entry struct {
	ID   int64  `json:"id"`
	Room string `json:"room"`
	Role string `json:"role"`
	// Author is the session that wrote the entry, kept for provenance only;
	// memory is keyed on Room and Role, never on Author.
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

// sessionPrefix marks a key derived from a session rather than a defined
// role, so the two can never collide.
const sessionPrefix = "session:"

// Key picks the identity memory is written under: role when a caller carries
// one, falling back to the session that is running otherwise. This is the
// whole policy for a room that defines no roles - a key derivation, not a
// separate code path - which is what lets any agent use memory before
// delegation exists.
func Key(role, sessionKey string) string {
	if role != "" {
		return role
	}
	return sessionPrefix + sessionKey
}
