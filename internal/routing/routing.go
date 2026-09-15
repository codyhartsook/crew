// Package routing decides what work leaves the main session: tier 1's
// advisory roster and tier 2's exact-match tool interception.
package routing

import (
	"regexp"
	"time"

	"github.com/codyhartsook/multiplayer/internal/role"
)

// Decision is one roster shown to one session on SessionStart.
type Decision struct {
	ID        int64     `json:"id"`
	Room      string    `json:"room"`
	Session   string    `json:"session"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"created_at"`
}

// MatchTool finds the first enforcing role whose tool trigger matches exactly.
func MatchTool(defs []role.Definition, toolName, target string) (role.Definition, bool) {
	for _, d := range defs {
		if d.Triggers.Mode != role.TriggerEnforce {
			continue
		}
		for _, t := range d.Triggers.Tool {
			if t.Tool != toolName {
				continue
			}
			re := t.Compiled()
			if re == nil {
				// Unset only for a hand-built definition (tests); fall back.
				var err error
				if re, err = regexp.Compile(t.Pattern); err != nil {
					continue
				}
			}
			if re.MatchString(target) {
				return d, true
			}
		}
	}
	return role.Definition{}, false
}
