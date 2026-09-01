package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/cli"
	"github.com/codyhartsook/multiplayer/internal/harness"
)

// runInstall executes the install command against a throwaway home directory
// and returns its output.
func runInstall(t *testing.T, home string, args ...string) string {
	t.Helper()
	t.Setenv("HOME", home)

	var out strings.Builder
	root := cli.New()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"install", "--binary", "/opt/bin/multiplayer"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("install %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func readHooks(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no hooks object: %s", path, raw)
	}
	return hooks
}

// commandsFor returns every hook command registered for an event.
func commandsFor(t *testing.T, hooks map[string]any, event string) []string {
	t.Helper()
	entries, ok := hooks[event].([]any)
	if !ok {
		t.Fatalf("no %s entries in %v", event, hooks)
	}
	var cmds []string
	for _, entry := range entries {
		group, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry is not an object: %v", entry)
		}
		specs, ok := group["hooks"].([]any)
		if !ok {
			t.Fatalf("entry has no hooks list: %v", group)
		}
		for _, spec := range specs {
			m, ok := spec.(map[string]any)
			if !ok {
				t.Fatalf("hook spec is not an object: %v", spec)
			}
			cmd, _ := m["command"].(string)
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// timeoutsFor returns the declared timeout of every hook registered for an event.
func timeoutsFor(t *testing.T, hooks map[string]any, event string) []float64 {
	t.Helper()
	entries, ok := hooks[event].([]any)
	if !ok {
		t.Fatalf("no %s entries in %v", event, hooks)
	}
	var out []float64
	for _, entry := range entries {
		for _, spec := range entry.(map[string]any)["hooks"].([]any) {
			timeout, _ := spec.(map[string]any)["timeout"].(float64)
			out = append(out, timeout)
		}
	}
	return out
}

// Each harness's hooks are installed with that harness's own timeouts. Sharing
// one number across harnesses is how Claude Code ended up on Codex's cap.
func TestInstallUsesPerHarnessTimeouts(t *testing.T) {
	home := t.TempDir()
	runInstall(t, home)

	for _, spec := range harness.Specs() {
		hooks := readHooks(t, filepath.Join(home, spec.ConfigPath))
		for event, want := range spec.Timeouts {
			got := timeoutsFor(t, hooks, event)
			if len(got) != 1 {
				t.Errorf("%s: %s has %d entries, want 1", spec.Harness, event, len(got))
				continue
			}
			if got[0] != float64(want) {
				t.Errorf("%s: %s timeout = %v, want its spec's %d", spec.Harness, event, got[0], want)
			}
		}
		// A session-start hook needs more headroom than the rest, since it is
		// the one doing the git and pool detection.
		if spec.Timeouts["SessionStart"] <= spec.Timeouts["SessionEnd"] {
			t.Errorf("%s: SessionStart timeout %d should exceed SessionEnd %d",
				spec.Harness, spec.Timeouts["SessionStart"], spec.Timeouts["SessionEnd"])
		}
	}
}

func TestInstallCreatesConfigs(t *testing.T) {
	home := t.TempDir()
	runInstall(t, home)

	cases := []struct {
		path    string
		harness string
	}{
		{filepath.Join(home, ".claude", "settings.json"), "claude"},
		{filepath.Join(home, ".codex", "hooks.json"), "codex"},
	}
	for _, tc := range cases {
		hooks := readHooks(t, tc.path)
		for _, event := range []string{"SessionStart", "SessionEnd"} {
			cmds := commandsFor(t, hooks, event)
			if len(cmds) != 1 {
				t.Fatalf("%s %s: got %d commands, want 1", tc.path, event, len(cmds))
			}
			// The path is quoted so a home directory with a space still works.
			if !strings.Contains(cmds[0], `"/opt/bin/multiplayer" hook`) {
				t.Errorf("%s %s: command = %q, want the quoted binary path", tc.path, event, cmds[0])
			}
			if !strings.Contains(cmds[0], "--harness "+tc.harness) {
				t.Errorf("%s %s: command = %q, want --harness %s", tc.path, event, cmds[0], tc.harness)
			}
		}
	}
}

// The Claude settings file holds far more than hooks, and other tools install
// hooks of their own. Neither may be disturbed.
func TestInstallPreservesExistingSettings(t *testing.T) {
	home := t.TempDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const existing = `{
  "model": "opus[1m]",
  "theme": "dark",
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "some-other-tool init"}]}
    ],
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "guard"}]}
    ]
  }
}`
	if err := os.WriteFile(settings, []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	runInstall(t, home, "--claude")

	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if config["model"] != "opus[1m]" || config["theme"] != "dark" {
		t.Errorf("unrelated settings lost: %s", raw)
	}

	hooks := config["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("PreToolUse hooks were dropped")
	}
	starts := commandsFor(t, hooks, "SessionStart")
	if len(starts) != 2 {
		t.Fatalf("SessionStart commands = %v, want the existing hook plus ours", starts)
	}
	if starts[0] != "some-other-tool init" {
		t.Errorf("existing hook = %q, want it preserved and first", starts[0])
	}

	matches, err := filepath.Glob(settings + ".bak-*")
	if err != nil || len(matches) != 1 {
		t.Errorf("expected exactly one backup, found %v (err %v)", matches, err)
	}
}

// Reinstalling must replace this tool's entries rather than stack up duplicates.
func TestInstallIsIdempotent(t *testing.T) {
	home := t.TempDir()
	runInstall(t, home, "--codex")
	out := runInstall(t, home, "--codex")

	if !strings.Contains(out, "already current") {
		t.Errorf("second install output = %q, want it to report no change", out)
	}
	hooks := readHooks(t, filepath.Join(home, ".codex", "hooks.json"))
	for _, event := range []string{"SessionStart", "SessionEnd"} {
		if cmds := commandsFor(t, hooks, event); len(cmds) != 1 {
			t.Errorf("%s commands = %v, want exactly one after reinstalling", event, cmds)
		}
	}
}

func TestInstallDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	out := runInstall(t, home, "--dry-run")

	if !strings.Contains(out, "would update") {
		t.Errorf("output = %q, want it to describe the pending change", out)
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "hooks.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("--dry-run created %s", path)
		}
	}
}

// --dry-run must report what would actually change, not claim a change on a
// config that is already current.
func TestInstallDryRunReportsNoChange(t *testing.T) {
	home := t.TempDir()
	runInstall(t, home)

	out := runInstall(t, home, "--dry-run")
	if strings.Contains(out, "would update") || strings.Contains(out, "would write skill") {
		t.Errorf("--dry-run on a current config claims a change:\n%s", out)
	}
	if !strings.Contains(out, "already current") {
		t.Errorf("--dry-run should report the config is current:\n%s", out)
	}
}
