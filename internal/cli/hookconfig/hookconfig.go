// Package hookconfig reads and rewrites the harness config files that hold the
// session hooks. init and uninstall are the same merge with and without
// entries, so both live here.
package hookconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// marker identifies hook entries this tool owns. It matches the flag
// combination rather than the binary path, which is quoted and may be
// reinstalled from elsewhere.
const marker = "hook --harness "

// managedEvents is every event this tool has ever installed. An event a harness
// no longer prices in its Timeouts is swept on init, so dropping one removes
// it rather than leaving an orphan hook firing with no way to uninstall it.
var managedEvents = []string{"SessionStart", "UserPromptSubmit", "SessionEnd"}

// Events lists the managed events, in firing order.
func Events() []string { return slices.Clone(managedEvents) }

// Installed reports whether hooks already carries our entry for event, exactly
// as Merge would write it, so init can say per hook whether it changed anything.
func Installed(hooks map[string]any, event, exe, harness string, timeout int) bool {
	entries, ok := hooks[event].([]any)
	if !ok {
		return false
	}
	want, err := json.Marshal(hookEntry(exe, harness, timeout))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !isOurs(entry) {
			continue
		}
		// Both sides go through encoding/json, which sorts keys, so an entry
		// read from disk compares equal to the one we would write.
		if got, err := json.Marshal(entry); err == nil && string(got) == string(want) {
			return true
		}
	}
	return false
}

// Merge applies this tool's entries to a hook map. Every managed event is
// visited, not just the tracked ones, so an event this tool has stopped
// installing is removed rather than orphaned. Other hooks are left alone.
func Merge(hooks map[string]any, timeouts map[string]int, exe, harness string) map[string]any {
	for _, name := range managedEvents {
		remaining := withoutOurs(hooks[name])
		if timeout, ok := timeouts[name]; ok {
			hooks[name] = append(remaining, hookEntry(exe, harness, timeout))
			continue
		}
		if len(remaining) == 0 {
			delete(hooks, name)
			continue
		}
		hooks[name] = remaining
	}
	return hooks
}

// Read loads path, treating a missing file as an empty object. The raw bytes
// come back too so an unchanged file can be left alone.
func Read(path string) ([]byte, map[string]any, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, map[string]any{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	config := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	return raw, config, nil
}

// Encode renders a config back to the on-disk form.
func Encode(config map[string]any) ([]byte, error) {
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return append(out, '\n'), nil
}

// MapAt returns the nested object at key, creating it when absent.
func MapAt(config map[string]any, key string) map[string]any {
	if existing, ok := config[key].(map[string]any); ok {
		return existing
	}
	return map[string]any{}
}

// hookEntry is the matcher group this tool installs. The matcher is omitted so
// the hook fires for every cause of the event.
func hookEntry(exe, harness string, timeout int) map[string]any {
	return map[string]any{
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       fmt.Sprintf("%q hook --harness %s --quiet", exe, harness),
				"timeout":       timeout,
				"statusMessage": "Registering agent session",
			},
		},
	}
}

// withoutOurs drops previously installed entries so reinstalling replaces
// rather than duplicates them, while leaving unrelated hooks in place.
func withoutOurs(existing any) []any {
	entries, ok := existing.([]any)
	if !ok {
		return nil
	}
	kept := make([]any, 0, len(entries))
	for _, entry := range entries {
		if !isOurs(entry) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// isOurs reports whether every command in a matcher group belongs to this tool.
// A group mixing our hook with someone else's is left alone rather than
// silently rewritten.
func isOurs(entry any) bool {
	group, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := group["hooks"].([]any)
	if !ok || len(hooks) == 0 {
		return false
	}
	for _, h := range hooks {
		spec, ok := h.(map[string]any)
		if !ok {
			return false
		}
		cmd, _ := spec["command"].(string)
		if !strings.Contains(cmd, marker) {
			return false
		}
	}
	return true
}
