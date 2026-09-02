// Package harness describes the coding-agent CLIs this registry supports.
// Everything that varies between them is one row of a table, so supporting
// another is a row rather than an edit in every package that cares.
package harness

import (
	"os"
	"slices"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// Spec is everything that varies between coding-agent CLIs.
type Spec struct {
	Harness session.Harness
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
	// Wake delivers a message into a live session, and is nil for a harness
	// with no inbound channel. Delivery says when a woken session acts on it,
	// and is empty when Wake is nil.
	Wake     Waker
	Delivery Delivery
}

var specs = []Spec{
	{
		Harness:    session.HarnessClaude,
		Binary:     "claude",
		EnvMarkers: []string{"CLAUDECODE", "CLAUDE_PROJECT_DIR", "CLAUDE_CODE_SESSION_ID"},
		SessionEnv: []string{"CLAUDE_CODE_SESSION_ID"},
		ConfigPath: ".claude/settings.json",
		ConfigRoot: "hooks",
		SkillsDir:  ".claude/skills",
		// Claude Code's hook timeout has a high ceiling, so the end path is not
		// squeezed the way Codex's is.
		EndBudget: 4 * time.Second,
		Timeouts:  map[string]int{"SessionStart": 10, "UserPromptSubmit": 5, "SessionEnd": 5},
		// No waker. Claude Code listens on a per-session unix socket, but it is
		// gated by a token held only in that process's environment and speaks an
		// undocumented protocol, so a broker cannot reach it. Claude sessions
		// wake each other through the harness's own cross-session messaging.
	},
	{
		Harness:     session.HarnessCodex,
		Binary:      "codex",
		EnvMarkers:  []string{"CODEX_HOME", "CODEX_SANDBOX", "CODEX_THREAD_ID"},
		SessionEnv:  []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID"},
		ConfigPath:  ".codex/hooks.json",
		ConfigRoot:  "hooks",
		SkillsDir:   ".codex/skills",
		SandboxTOML: ".codex/config.toml",
		// Codex clamps SessionEnd to three seconds and warns above it.
		EndBudget: 2500 * time.Millisecond,
		Timeouts:  map[string]int{"SessionStart": 10, "UserPromptSubmit": 5, "SessionEnd": 3},
		Wake:      wakeCodex,
		Delivery:  DeliverOnIdle,
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
