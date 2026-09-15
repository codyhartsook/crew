package role

import "time"

// Activation records that a room has turned on a role. Explicit opt-in: a
// discovered role is dormant until this exists.
type Activation struct {
	Room        string    `json:"room"`
	Role        string    `json:"role"`
	ActivatedAt time.Time `json:"activated_at"`
}
