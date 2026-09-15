// Package role is the registry of role identities: name, harness, briefing.
package role

import (
	"fmt"
	"regexp"
	"strings"
)

// Scope distinguishes where a definition came from. Repo wins on collision.
type Scope string

const (
	ScopeRepo   Scope = "repo"
	ScopeGlobal Scope = "global"
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

// TriggerMode decides whether a match only suggests, or blocks the call.
type TriggerMode string

const (
	TriggerSuggest TriggerMode = "suggest"
	TriggerEnforce TriggerMode = "enforce"
)

func (m TriggerMode) Valid() bool {
	switch m {
	case TriggerSuggest, TriggerEnforce:
		return true
	default:
		return false
	}
}

// ToolTrigger matches an exact tool call. Pattern matches the tool's input.
type ToolTrigger struct {
	Tool    string `toml:"tool" json:"tool"`
	Pattern string `toml:"pattern" json:"pattern"`

	// compiled once by Validate; avoids recompiling on every tool call.
	compiled *regexp.Regexp
}

// Compiled returns the pattern Validate compiled, or nil if that never ran.
func (t ToolTrigger) Compiled() *regexp.Regexp { return t.compiled }

// Triggers is a definition's routing signal: Prompt is advisory, Tool is exact.
type Triggers struct {
	Prompt []string      `toml:"prompt" json:"prompt,omitempty"`
	Tool   []ToolTrigger `toml:"tool" json:"tool,omitempty"`
	Mode   TriggerMode   `toml:"mode" json:"mode"`
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
	Name         string   `toml:"name" json:"name"`
	Description  string   `toml:"description" json:"description"`
	Harness      Harness  `toml:"harness" json:"harness"`
	Memory       Memory   `toml:"memory" json:"memory"`
	Triggers     Triggers `toml:"triggers" json:"triggers"`
	Render       Render   `toml:"render" json:"render"`
	Output       Output   `toml:"output" json:"output"`
	Instructions string   `toml:"instructions" json:"instructions"`

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
	if d.Triggers.Mode != "" && !d.Triggers.Mode.Valid() {
		problems = append(problems, fmt.Sprintf("triggers.mode %q must be suggest or enforce", d.Triggers.Mode))
	}
	for _, p := range d.Triggers.Prompt {
		if _, err := regexp.Compile(p); err != nil {
			problems = append(problems, fmt.Sprintf("triggers.prompt %q: %v", p, err))
		}
	}
	for i := range d.Triggers.Tool {
		t := &d.Triggers.Tool[i]
		if t.Tool == "" || t.Pattern == "" {
			problems = append(problems, "triggers.tool entries need both tool and pattern")
			continue
		}
		re, err := regexp.Compile(t.Pattern)
		if err != nil {
			problems = append(problems, fmt.Sprintf("triggers.tool %s pattern %q: %v", t.Tool, t.Pattern, err))
			continue
		}
		t.compiled = re
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", d.Path, strings.Join(problems, "; "))
}

// normalize defaults an unset trigger mode to suggest.
func (d *Definition) normalize() {
	if d.Triggers.Mode == "" {
		d.Triggers.Mode = TriggerSuggest
	}
}
