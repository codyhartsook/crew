package harness_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// managedEvents are the events init registers. Every harness must price all
// of them, or a hook would be silently left uninstalled for that harness.
var managedEvents = []string{"SessionStart", "UserPromptSubmit", "SessionEnd"}

// clearMarkers blanks every harness's env markers. The tests run inside a
// harness, so its variables are inherited and would decide Detect for us.
func clearMarkers(t *testing.T) {
	t.Helper()
	for _, s := range harness.Specs() {
		for _, env := range s.EnvMarkers {
			t.Setenv(env, "")
		}
		for _, env := range s.SessionEnv {
			t.Setenv(env, "")
		}
	}
}

func TestDetectFromEnvMarkers(t *testing.T) {
	for _, spec := range harness.Specs() {
		for _, env := range spec.EnvMarkers {
			t.Run(string(spec.Harness)+"/"+env, func(t *testing.T) {
				clearMarkers(t)
				t.Setenv(env, "1")
				if got := harness.Detect(); got != spec.Harness {
					t.Errorf("Detect() = %q, want %q from %s", got, spec.Harness, env)
				}
			})
		}
	}
}

func TestDetectWithoutMarkers(t *testing.T) {
	clearMarkers(t)
	if got := harness.Detect(); got != session.HarnessUnknown {
		t.Errorf("Detect() = %q, want %q", got, session.HarnessUnknown)
	}
}

func TestForKnownAndUnknown(t *testing.T) {
	for _, h := range []session.Harness{session.HarnessClaude, session.HarnessCodex} {
		spec, ok := harness.For(h)
		if !ok {
			t.Errorf("For(%q) not found", h)
			continue
		}
		if spec.Harness != h {
			t.Errorf("For(%q).Harness = %q", h, spec.Harness)
		}
	}
	if _, ok := harness.For(session.HarnessUnknown); ok {
		t.Error("For(unknown) returned a spec")
	}
}

// A half-filled row is the way this table goes wrong, so every field the rest of
// the codebase depends on is required.
func TestSpecsAreComplete(t *testing.T) {
	for _, s := range harness.Specs() {
		t.Run(string(s.Harness), func(t *testing.T) {
			if !s.Harness.Valid() || s.Harness == session.HarnessUnknown {
				t.Errorf("Harness = %q, want a real harness", s.Harness)
			}
			for name, got := range map[string]string{
				"Label":      s.Label,
				"Binary":     s.Binary,
				"ConfigPath": s.ConfigPath,
				"ConfigRoot": s.ConfigRoot,
				"SkillsDir":  s.SkillsDir,
			} {
				if got == "" {
					t.Errorf("%s is empty", name)
				}
			}
			if len(s.EnvMarkers) == 0 {
				t.Error("EnvMarkers is empty, so the harness cannot be detected")
			}
			if len(s.SessionEnv) == 0 {
				t.Error("SessionEnv is empty, so an agent cannot identify itself")
			}
			if s.EndBudget <= 0 {
				t.Errorf("EndBudget = %s, want positive", s.EndBudget)
			}
			for _, event := range managedEvents {
				if s.Timeouts[event] <= 0 {
					t.Errorf("Timeouts[%s] = %d, want positive", event, s.Timeouts[event])
				}
			}
		})
	}
}

// Specs hands out a copy, so a caller cannot reorder the table that Detect uses.
func TestSpecsIsACopy(t *testing.T) {
	got := harness.Specs()
	if len(got) < 2 {
		t.Fatalf("Specs() = %d rows, want at least 2", len(got))
	}
	got[0] = harness.Spec{}
	if harness.Specs()[0].Harness == "" {
		t.Error("mutating the result changed the table")
	}
}

// The hook must finish before the harness kills it, so each end budget has to
// fit inside the timeout its own SessionEnd hook is installed with.
func TestEndBudgetFitsInstalledTimeout(t *testing.T) {
	for _, s := range harness.Specs() {
		installed := time.Duration(s.Timeouts["SessionEnd"]) * time.Second
		if s.EndBudget >= installed {
			t.Errorf("%s: EndBudget %v does not fit the installed SessionEnd timeout %v",
				s.Harness, s.EndBudget, installed)
		}
	}
}

func TestCodexSkillsFollowCodexHome(t *testing.T) {
	spec, _ := harness.For(session.HarnessCodex)
	t.Setenv("CODEX_HOME", "")
	if got := spec.SkillsPath("/h"); got != filepath.Join("/h", ".codex", "skills") {
		t.Errorf("default = %q", got)
	}
	t.Setenv("CODEX_HOME", "/c")
	if got := spec.SkillsPath("/h"); got != filepath.Join("/c", "skills") {
		t.Errorf("with CODEX_HOME = %q", got)
	}
}
