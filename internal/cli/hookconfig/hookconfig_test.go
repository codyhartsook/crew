package hookconfig

import (
	"encoding/json"
	"slices"
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
	  "Stop":             [{"hooks": [{"type":"command","command":"\"/old/mp\" hook --harness codex --quiet"}]}]
	}`)

	// UserPromptSubmit is managed but no longer tracked: sweep it.
	got := Merge(hooks, map[string]int{"SessionStart": 10}, "/new/mp", "codex")

	ups := commands(t, got, "UserPromptSubmit")
	if len(ups) != 1 || ups[0] != "someone-else --watch" {
		t.Errorf("UserPromptSubmit = %v, want only the unrelated hook", ups)
	}
	starts := commands(t, got, "SessionStart")
	if len(starts) != 1 || starts[0] != `"/new/mp" hook --harness codex --quiet` {
		t.Errorf("SessionStart = %v, want the reinstalled hook at the new path", starts)
	}
	// An event this tool never managed is not its business to touch.
	if len(commands(t, got, "Stop")) != 1 {
		t.Error("Stop was modified; only managed events should be swept")
	}
}

func TestMergeHooksDropsEmptiedEvents(t *testing.T) {
	hooks := hookMap(t, `{"UserPromptSubmit": [
	  {"hooks": [{"type":"command","command":"\"/old/mp\" hook --harness codex --quiet"}]}
	]}`)

	got := Merge(hooks, map[string]int{"SessionStart": 10}, "/new/mp", "codex")
	if _, present := got["UserPromptSubmit"]; present {
		t.Error("UserPromptSubmit key remains after its only hook was swept")
	}
}

// Reinstalling replaces this tool's entry rather than stacking another copy.
func TestMergeHooksIsIdempotent(t *testing.T) {
	tracked := map[string]int{"SessionStart": 10}
	hooks := Merge(map[string]any{}, tracked, "/new/mp", "codex")
	hooks = Merge(hooks, tracked, "/new/mp", "codex")

	if got := commands(t, hooks, "SessionStart"); len(got) != 1 {
		t.Errorf("SessionStart = %v, want one entry after reinstalling", got)
	}
}

// Uninstall should leave no trace: no empty hooks object in a shared settings
// file, and no empty file where the file was only ever ours.
func TestMergeHooksEmptiesCleanly(t *testing.T) {
	hooks := hookMap(t, `{
	  "SessionStart": [{"hooks": [{"type":"command","command":"\"/mp\" hook --harness codex --quiet"}]}],
	  "SessionEnd":   [{"hooks": [{"type":"command","command":"\"/mp\" hook --harness codex --quiet"}]}]
	}`)
	if got := Merge(hooks, nil, "", "codex"); len(got) != 0 {
		t.Errorf("mergeHooks with nothing tracked = %v, want empty", got)
	}

	shared := hookMap(t, `{
	  "SessionStart": [{"hooks": [{"type":"command","command":"\"/mp\" hook --harness codex --quiet"}]}],
	  "PreToolUse":   [{"hooks": [{"type":"command","command":"someone-else"}]}]
	}`)
	got := Merge(shared, nil, "", "codex")
	if len(got) != 1 {
		t.Fatalf("mergeHooks = %v, want only the unrelated event left", got)
	}
	if _, ok := got["PreToolUse"]; !ok {
		t.Error("the unrelated event was removed")
	}
}

// Installed must recognize the entry we would write, read back through JSON,
// and nothing else.
func TestInstalledRecognizesOnlyACurrentEntry(t *testing.T) {
	hooks := Merge(map[string]any{}, map[string]int{"SessionStart": 10}, "/bin/mp", "codex")
	// Round-trip as init sees a config: timeouts come back as float64.
	raw, err := json.Marshal(hooks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	hooks = hookMap(t, string(raw))

	if !Installed(hooks, "SessionStart", "/bin/mp", "codex", 10) {
		t.Error("the entry Merge just wrote is not reported as installed")
	}
	for _, tc := range []struct {
		name    string
		exe     string
		harness string
		timeout int
	}{
		{"moved binary", "/usr/local/bin/mp", "codex", 10},
		{"other harness", "/bin/mp", "claude", 10},
		{"retimed hook", "/bin/mp", "codex", 5},
	} {
		if Installed(hooks, "SessionStart", tc.exe, tc.harness, tc.timeout) {
			t.Errorf("%s: reported as installed, want a rewrite", tc.name)
		}
	}
	if Installed(hooks, "SessionEnd", "/bin/mp", "codex", 3) {
		t.Error("an event with no entry is reported as installed")
	}
}

// A foreign hook on a managed event is not ours, however it is shaped.
func TestInstalledIgnoresForeignEntries(t *testing.T) {
	hooks := hookMap(t, `{"SessionStart": [
	  {"hooks": [{"type":"command","command":"someone-else --watch","timeout":10}]}
	]}`)
	if Installed(hooks, "SessionStart", "/bin/mp", "codex", 10) {
		t.Error("someone else's hook was read as ours")
	}
}

// init walks Events to report a hook per line, so order matters.
func TestEventsIsAnOrderedCopy(t *testing.T) {
	got := Events()
	want := []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "SessionEnd"}
	if !slices.Equal(got, want) {
		t.Errorf("Events() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if Events()[0] != "SessionStart" {
		t.Error("mutating the result changed the managed list")
	}
}
