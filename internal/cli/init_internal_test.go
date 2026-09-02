package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookMap builds a hook map from JSON, as it would be read from a config file.
func hookMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return out
}

func commands(t *testing.T, hooks map[string]any, event string) []string {
	t.Helper()
	entries, ok := hooks[event].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, entry := range entries {
		for _, spec := range entry.(map[string]any)["hooks"].([]any) {
			cmd, _ := spec.(map[string]any)["command"].(string)
			out = append(out, cmd)
		}
	}
	return out
}

// An event this tool has stopped installing must have its entry removed, not
// left firing with no way to uninstall it. Tracked separately from whatever
// happens to be installed today, so retiring an event stays covered.
func TestMergeHooksSweepsRetiredEvents(t *testing.T) {
	hooks := hookMap(t, `{
	  "SessionStart":     [{"hooks": [{"type":"command","command":"\"/old/mp\" hook --harness codex --quiet"}]}],
	  "UserPromptSubmit": [
	    {"hooks": [{"type":"command","command":"\"/old/mp\" hook --harness codex --quiet"}]},
	    {"hooks": [{"type":"command","command":"someone-else --watch"}]}
	  ],
	  "PreToolUse":       [{"hooks": [{"type":"command","command":"\"/old/mp\" hook --harness codex --quiet"}]}]
	}`)

	// UserPromptSubmit is managed but no longer tracked: sweep it.
	got := mergeHooks(hooks, map[string]int{"SessionStart": 10}, "/new/mp", "codex")

	ups := commands(t, got, "UserPromptSubmit")
	if len(ups) != 1 || ups[0] != "someone-else --watch" {
		t.Errorf("UserPromptSubmit = %v, want only the unrelated hook", ups)
	}
	starts := commands(t, got, "SessionStart")
	if len(starts) != 1 || starts[0] != `"/new/mp" hook --harness codex --quiet` {
		t.Errorf("SessionStart = %v, want the reinstalled hook at the new path", starts)
	}
	// An event this tool never managed is not its business to touch.
	if len(commands(t, got, "PreToolUse")) != 1 {
		t.Error("PreToolUse was modified; only managed events should be swept")
	}
}

func TestMergeHooksDropsEmptiedEvents(t *testing.T) {
	hooks := hookMap(t, `{"UserPromptSubmit": [
	  {"hooks": [{"type":"command","command":"\"/old/mp\" hook --harness codex --quiet"}]}
	]}`)

	got := mergeHooks(hooks, map[string]int{"SessionStart": 10}, "/new/mp", "codex")
	if _, present := got["UserPromptSubmit"]; present {
		t.Error("UserPromptSubmit key remains after its only hook was swept")
	}
}

// Reinstalling replaces this tool's entry rather than stacking another copy.
func TestMergeHooksIsIdempotent(t *testing.T) {
	tracked := map[string]int{"SessionStart": 10}
	hooks := mergeHooks(map[string]any{}, tracked, "/new/mp", "codex")
	hooks = mergeHooks(hooks, tracked, "/new/mp", "codex")

	if got := commands(t, hooks, "SessionStart"); len(got) != 1 {
		t.Errorf("SessionStart = %v, want one entry after reinstalling", got)
	}
}

// A "go run" binary lives in the build cache and is deleted on exit; recording
// its path would leave hooks that fail silently forever.
func TestTransient(t *testing.T) {
	for _, path := range []string{
		"/var/folders/xx/T/go-build123/b001/exe/multiplayer",
		filepath.Join(os.TempDir(), "mp"),
	} {
		if !transient(path) {
			t.Errorf("transient(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"/usr/local/bin/multiplayer", "/Users/x/.local/bin/multiplayer"} {
		if transient(path) {
			t.Errorf("transient(%q) = true, want false", path)
		}
	}
}

func TestInitViewStaysPlainOutsideATerminal(t *testing.T) {
	var out strings.Builder
	view := newInitView(&out)
	if got := view.heading("Setup"); got != "Setup" {
		t.Errorf("heading = %q, want plain text", got)
	}
}

// The skill is meant to be tuned once you see what agents write, so a re-init
// must not overwrite an edit.
func TestInitSkillPreservesLocalEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if got, err := initSkill(path, false); err != nil || got != skillWritten {
		t.Fatalf("first init = (%v, %v), want written", got, err)
	}
	if got, err := initSkill(path, false); err != nil || got != skillUnchanged {
		t.Fatalf("re-init unchanged = (%v, %v), want unchanged", got, err)
	}

	// An older version this tool wrote is safe to replace.
	if err := os.WriteFile(path, []byte("an older generated skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, initMarker), []byte(digest([]byte("an older generated skill"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := initSkill(path, false); err != nil || got != skillWritten {
		t.Fatalf("replacing our own older output = (%v, %v), want written", got, err)
	}

	// An edit is not.
	edited := append(append([]byte{}, skillDoc...), []byte("\nMY TUNING\n")...)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := initSkill(path, false)
	if err != nil || got != skillPreserved {
		t.Fatalf("re-init over an edit = (%v, %v), want preserved", got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(after), "MY TUNING") {
		t.Error("the local edit was overwritten")
	}
	if _, err := os.Stat(path + ".new"); err != nil {
		t.Error("no .new written alongside the preserved skill")
	}
}

// A skill initialized before markers existed is still ours, and must not be
// mistaken for a local edit the first time it needs updating.
func TestInitSkillAdoptsAnUnmarkedCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, skillDoc, 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := initSkill(path, false); err != nil || got != skillUnchanged {
		t.Fatalf("init over an identical unmarked copy = (%v, %v), want unchanged", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, initMarker)); err != nil {
		t.Fatal("no marker recorded for a copy we recognised as ours")
	}
	// Now a genuine update replaces it rather than preserving it.
	if err := os.WriteFile(path, []byte("pretend this is an older generated version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, initMarker), []byte(digest([]byte("pretend this is an older generated version"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := initSkill(path, false); err != nil || got != skillWritten {
		t.Fatalf("update = (%v, %v), want written", got, err)
	}
}

// Uninstall should leave no trace: no empty hooks object in a shared settings
// file, and no empty file where the file was only ever ours.
func TestMergeHooksEmptiesCleanly(t *testing.T) {
	hooks := hookMap(t, `{
	  "SessionStart": [{"hooks": [{"type":"command","command":"\"/mp\" hook --harness codex --quiet"}]}],
	  "SessionEnd":   [{"hooks": [{"type":"command","command":"\"/mp\" hook --harness codex --quiet"}]}]
	}`)
	if got := mergeHooks(hooks, nil, "", "codex"); len(got) != 0 {
		t.Errorf("mergeHooks with nothing tracked = %v, want empty", got)
	}

	shared := hookMap(t, `{
	  "SessionStart": [{"hooks": [{"type":"command","command":"\"/mp\" hook --harness codex --quiet"}]}],
	  "PreToolUse":   [{"hooks": [{"type":"command","command":"someone-else"}]}]
	}`)
	got := mergeHooks(shared, nil, "", "codex")
	if len(got) != 1 {
		t.Fatalf("mergeHooks = %v, want only the unrelated event left", got)
	}
	if _, ok := got["PreToolUse"]; !ok {
		t.Error("the unrelated event was removed")
	}
}
