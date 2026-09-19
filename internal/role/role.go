// Package role is the registry of role identities: name, harness, briefing.
package role

import (
	"fmt"
	"regexp"
	"strings"
)

// Scope distinguishes where a definition came from. Priority: repo, global, embedded.
type Scope string

const (
	ScopeRepo     Scope = "repo"
	ScopeGlobal   Scope = "global"
	ScopeEmbedded Scope = "embedded"
)

// Harness names which agent CLI runs a role. "any" defers the choice.
type Harness string

const (
	HarnessClaude Harness = "claude"
	HarnessCodex  Harness = "codex"
	HarnessAny    Harness = "any"
)

func (h Harness) Valid() bool {
	switch h {
	case HarnessClaude, HarnessCodex, HarnessAny:
		return true
	default:
		return false
	}
}

// Memory names where a role's knowledge persists across spawns.
type Memory string

const (
	MemoryRole    Memory = "role"
	MemorySession Memory = "session"
	MemoryNone    Memory = "none"
)

func (m Memory) Valid() bool {
	switch m {
	case MemoryRole, MemorySession, MemoryNone:
		return true
	default:
		return false
	}
}

// Render is spawn config; each harness takes the fields that apply to it.
type Render struct {
	Model   string   `toml:"model" json:"model,omitempty"`
	Sandbox string   `toml:"sandbox" json:"sandbox,omitempty"`
	Tools   []string `toml:"tools" json:"tools,omitempty"`
	Effort  string   `toml:"effort" json:"effort,omitempty"`
}

// Output bounds what a spawned role may return.
type Output struct {
	Schema string `toml:"schema" json:"schema,omitempty"`
}

// Definition is one role, as read from a .crew/agents/*.toml file.
type Definition struct {
	Name         string  `toml:"name" json:"name"`
	Description  string  `toml:"description" json:"description"`
	Harness      Harness `toml:"harness" json:"harness"`
	Memory       Memory  `toml:"memory" json:"memory"`
	Render       Render  `toml:"render" json:"render"`
	Output       Output  `toml:"output" json:"output"`
	Instructions string  `toml:"instructions" json:"instructions"`

	// Scope and Path are filled in by the loader, not the file.
	Scope Scope  `toml:"-" json:"scope"`
	Path  string `toml:"-" json:"path"`
}

// nameRE keeps a role's name usable as a CLI argument and a file stem.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate reports every problem at once, naming the source file.
func (d *Definition) Validate() error {
	var problems []string
	if d.Name == "" {
		problems = append(problems, "name is required")
	} else if !nameRE.MatchString(d.Name) {
		problems = append(problems, fmt.Sprintf("name %q must be lowercase letters, digits, and dashes, starting with a letter", d.Name))
	}
	if d.Description == "" {
		problems = append(problems, "description is required")
	}
	if !d.Harness.Valid() {
		problems = append(problems, fmt.Sprintf("harness %q must be claude, codex, or any", d.Harness))
	}
	if !d.Memory.Valid() {
		problems = append(problems, fmt.Sprintf("memory %q must be role, session, or none", d.Memory))
	}
	if d.Instructions == "" {
		problems = append(problems, "instructions is required")
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", d.Path, strings.Join(problems, "; "))
}
