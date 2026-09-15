package role

import "time"

// Activation records that a room has enabled a role: it is what makes a
// defined role part of the roster a session in that room sees on
// SessionStart. A discovered role is dormant until this exists for it -
// activation is explicit opt-in, kept separate from the definition itself, so
// an unfiltered roster never spends a session's context on roles nobody
// there uses.
type Activation struct {
	Room        string    `json:"room"`
	Role        string    `json:"role"`
	ActivatedAt time.Time `json:"activated_at"`
}
