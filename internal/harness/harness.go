// Package harness describes the coding-agent CLIs this registry supports.
// Everything that varies between them is one row of a table, so supporting
// another is a row rather than an edit in every package that cares.
package harness

import (
	"os"
	"slices"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness/claude"
	"github.com/codyhartsook/multiplayer/internal/harness/codex"
	"github.com/codyhartsook/multiplayer/internal/harness/notifier"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

// Spec is everything that varies between coding-agent CLIs.
type Spec struct {
	Harness session.Harness
	// Label is the harness's own name, for output a person reads.
	Label string
	// Binary is the process name a running session is recognized by. It is not
	// required to match the registry key.
	Binary string
	// EnvMarkers identify the harness calling a hook; any one being set is enough.
	EnvMarkers []string
	// SessionEnv holds the harness's own session-id variables, in preference
	// order. Exact, and unlike process inspection it survives a sandbox.
	SessionEnv []string
	// ConfigPath is the hook configuration file, relative to home, and
	// ConfigRoot the key within it holding the hook map.
	ConfigPath string
	ConfigRoot string
	// SkillsDir is the harness's global skills directory, relative to home.
	SkillsDir string
	// SandboxTOML is the config whose sandbox needs write access to the store.
	// Empty for a harness that does not sandbox.
	SandboxTOML string
	// EndBudget caps the session-end hook, which runs in front of the user.
	EndBudget time.Duration
	// Timeouts is the timeout each lifecycle hook is installed with, keyed by
	// wire event name. An event missing here is not installed.
	Timeouts map[string]int
	// Notify delivers a message into a live session, and is nil when unsupported.
	Notify notifier.Notifier
	// Usage samples this harness's local usage data, and is nil when unavailable.
	Usage usage.Source
}

var specs = []Spec{
	{
		Harness:    session.HarnessClaude,
		Label:      "Claude Code",
		Binary:     "claude",
		EnvMarkers: []string{"CLAUDECODE", "CLAUDE_PROJECT_DIR", "CLAUDE_CODE_SESSION_ID"},
		SessionEnv: []string{"CLAUDE_CODE_SESSION_ID"},
		ConfigPath: ".claude/settings.json",
		ConfigRoot: "hooks",
		SkillsDir:  ".claude/skills",
		// Claude Code's hook timeout has a high ceiling, so the end path is not
		// squeezed the way Codex's is.
		EndBudget: 4 * time.Second,
		Timeouts:  map[string]int{"SessionStart": 10, "UserPromptSubmit": 5, "PreToolUse": 3, "SessionEnd": 5},
		Notify:    claude.Notify,
		Usage:     usage.ClaudeSource{},
	},
	{
		Harness:     session.HarnessCodex,
		Label:       "Codex",
		Binary:      "codex",
		EnvMarkers:  []string{"CODEX_HOME", "CODEX_SANDBOX", "CODEX_THREAD_ID"},
		SessionEnv:  []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID"},
		ConfigPath:  ".codex/hooks.json",
		ConfigRoot:  "hooks",
		SkillsDir:   ".codex/skills",
		SandboxTOML: ".codex/config.toml",
		// Codex clamps SessionEnd to three seconds and warns above it.
		EndBudget: 2500 * time.Millisecond,
		Timeouts:  map[string]int{"SessionStart": 10, "UserPromptSubmit": 5, "PreToolUse": 3, "SessionEnd": 3},
		Notify:    codex.Notify,
		Usage:     usage.CodexSource{},
	},
}

// Specs lists every supported harness, in a stable order.
func Specs() []Spec { return slices.Clone(specs) }

// For returns the spec for a harness.
func For(h session.Harness) (Spec, bool) {
	for _, s := range specs {
		if s.Harness == h {
			return s, true
		}
	}
	return Spec{}, false
}

// Detect infers the calling harness from the environment. The hook
// configuration names the harness explicitly, so this only covers a hook
// invoked without that flag.
func Detect() session.Harness {
	for _, s := range specs {
		for _, env := range s.EnvMarkers {
			if os.Getenv(env) != "" {
				return s.Harness
			}
		}
	}
	return session.HarnessUnknown
}
