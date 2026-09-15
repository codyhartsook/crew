// Package role is the registry of role identities crew's delegation system
// reads. A definition names a role, which harness runs it, and how it is
// briefed; nothing in this package changes runtime behavior on its own.
package role

import (
	"fmt"
	"regexp"
	"strings"
)

// Scope distinguishes where a definition came from. Both are searched;
// ScopeRepo wins on a name collision, since a repo definition is
// version-controlled and reviewable.
type Scope string

const (
	ScopeRepo   Scope = "repo"
	ScopeGlobal Scope = "global"
)

// Harness names which agent CLI a role runs under. "any" defers the choice to
// whatever spawns the role.
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

// TriggerMode decides whether a matched trigger only suggests delegating, or
// blocks the inline call. Enforce has no effect before phase 6 builds the
// interception it requires, but a definition can declare it in advance.
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

// ToolTrigger matches an exact tool call, for the structured interception
// phase 6 adds. Pattern is matched against the tool's input, not its name.
type ToolTrigger struct {
	Tool    string `toml:"tool" json:"tool"`
	Pattern string `toml:"pattern" json:"pattern"`

	// compiled is set once by Validate, the only place Pattern is known to be
	// valid, so a hot path matching every tool call need not recompile it.
	compiled *regexp.Regexp
}

// Compiled returns the trigger's pattern already compiled by Validate, or nil
// if that never ran (a definition built by hand rather than loaded).
func (t ToolTrigger) Compiled() *regexp.Regexp { return t.compiled }

// Triggers is the routing signal a definition carries. Prompt patterns are
// advisory (tier 1); Tool patterns are exact matches structured interception
// can act on (tier 2).
type Triggers struct {
	Prompt []string      `toml:"prompt" json:"prompt,omitempty"`
	Tool   []ToolTrigger `toml:"tool" json:"tool,omitempty"`
	Mode   TriggerMode   `toml:"mode" json:"mode"`
}

// Render is spawn configuration. Fields carry both harnesses' vocabulary
// rather than a lowest common denominator; each spawn path takes what
// applies to it.
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

	// Scope and Path are not read from the file; the loader fills them in so
	// an error, or a listing, can name where a definition came from.
	Scope Scope  `toml:"-" json:"scope"`
	Path  string `toml:"-" json:"path"`
}

// nameRE keeps a role's name usable as a CLI argument and a file stem.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate checks a definition for the mistakes that must fail at load rather
// than surface later as a confusing spawn failure. Every problem is reported
// together, and the error always names the source file.
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

// normalize fills in defaults that would otherwise make an unset field read
// as an error. Enforce is opt-in, so an unset mode means suggest.
func (d *Definition) normalize() {
	if d.Triggers.Mode == "" {
		d.Triggers.Mode = TriggerSuggest
	}
}
