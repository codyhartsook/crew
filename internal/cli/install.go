package cli

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// marker identifies hook entries this tool owns. It matches the flag
// combination rather than the binary path, which is quoted and may be
// reinstalled from elsewhere.
const marker = "hook --harness "

// trackedEvents are the lifecycle events the registry needs, with each one's
// timeout. Codex clamps SessionEnd to three seconds and warns above it; the end
// path does no git work, so three is ample.
var trackedEvents = []eventSpec{
	{"SessionStart", 10},
	{"UserPromptSubmit", 5},
	{"SessionEnd", 3},
}

// eventSpec is one hook event and the timeout it is installed with.
type eventSpec struct {
	name    string
	timeout int
}

// managedEvents is every event this tool has ever installed. Events no longer
// in trackedEvents are swept on install, so dropping one removes it rather than
// leaving an orphan hook firing with no way to uninstall it.
var managedEvents = []string{"SessionStart", "UserPromptSubmit", "SessionEnd"}

// installedMarker records the hash of the skill this tool last wrote, so a
// reinstall can tell its own previous output from something you edited.
const installedMarker = ".installed"

// skillOutcome is what happened to a skill file.
type skillOutcome int

const (
	skillUnchanged skillOutcome = iota
	skillWritten
	skillPreserved // you edited it; a new version was left alongside
)

//go:embed skill.md
var skillDoc []byte

// skillName is the directory the skill is installed under in both harnesses.
const skillName = "multiplayer-rooms"

// target is one harness's hook configuration file.
type target struct {
	name string
	// skills is the harness's global skills directory, relative to home.
	skills string
	// sandbox is the config file whose sandbox needs write access to the
	// store, relative to home. Empty for harnesses that do not sandbox.
	sandbox string
	// path is the config file, relative to the user's home directory.
	path string
	// root is the JSON key holding the hook map. Claude Code keeps hooks inside
	// its wider settings file; Codex uses a dedicated file whose top level is
	// the same object.
	root string
	// harness is the value passed to "multiplayer hook --harness".
	harness string
}

var targets = []target{
	{name: "claude", path: ".claude/settings.json", root: "hooks", harness: "claude", skills: ".claude/skills"},
	{name: "codex", path: ".codex/hooks.json", root: "hooks", harness: "codex", skills: ".codex/skills", sandbox: ".codex/config.toml"},
}

func newInstallCmd(opts *options) *cobra.Command {
	var (
		claudeOnly bool
		codexOnly  bool
		binary     string
		dryRun     bool
		noSandbox  bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register the session hooks with Claude Code and Codex",
		Long: `Adds the session hooks pointing at this binary, writes the room skill,
and grants Codex's sandbox write access to the store directory, without which
every room write from a Codex agent fails as a readonly database.

Reinstalling is safe: our entries are replaced, others untouched, files backed up.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			exe := binary
			if exe == "" {
				resolved, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolve this binary's path: %w", err)
				}
				if transient(resolved) {
					return fmt.Errorf("this binary is temporary (%s) and the hooks would point at a path that stops existing.\nBuild it first:\n  go build -o ~/.local/bin/multiplayer ./cmd/multiplayer && ~/.local/bin/multiplayer install\nor pass --binary <path>", resolved)
				}
				exe = resolved
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}

			selected := targets
			if claudeOnly != codexOnly {
				name := "claude"
				if codexOnly {
					name = "codex"
				}
				selected = filterTargets(name)
			}

			out := cmd.OutOrStdout()
			for _, t := range selected {
				path := filepath.Join(home, t.path)
				changed, err := installInto(path, t, exe, dryRun)
				if err != nil {
					return fmt.Errorf("%s: %w", t.name, err)
				}
				if t.sandbox != "" && !noSandbox {
					storeDir, err := opts.storeDir()
					if err != nil {
						return err
					}
					sandboxPath := filepath.Join(home, t.sandbox)
					granted, err := ensureWritableRoot(sandboxPath, storeDir, dryRun)
					if err != nil {
						return fmt.Errorf("%s sandbox: %w", t.name, err)
					}
					if granted {
						verb := "sandbox write access granted for"
						if dryRun {
							verb = "would grant sandbox write access for"
						}
						fmt.Fprintf(out, "%s: %s %s in %s\n", t.name, verb, storeDir, sandboxPath)
					}
				}
				skillPath := filepath.Join(home, t.skills, skillName, "SKILL.md")
				_, previous := readMarker(skillPath)
				outcome, err := installSkill(skillPath, dryRun)
				if err != nil {
					return fmt.Errorf("%s skill: %w", t.name, err)
				}
				switch {
				case outcome == skillPreserved:
					fmt.Fprintf(out, "%s: skill has local edits, left alone; new version at %s.new\n", t.name, skillPath)
				case outcome == skillWritten && dryRun:
					fmt.Fprintf(out, "%s: would write skill to %s\n", t.name, skillPath)
				case outcome == skillWritten:
					fmt.Fprintf(out, "%s: skill installed in %s%s\n", t.name, skillPath, replacing(previous))
				}
				switch {
				case !changed:
					fmt.Fprintf(out, "%s: hooks already current in %s\n", t.name, path)
				case dryRun:
					fmt.Fprintf(out, "%s: would update %s\n", t.name, path)
				default:
					fmt.Fprintf(out, "%s: hooks installed in %s\n", t.name, path)
				}
			}
			if !dryRun {
				fmt.Fprintln(out, "\nStart a new session in either harness, then run: multiplayer ls")
				fmt.Fprintln(out, "Codex asks to trust a newly added hook the first time it runs.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&claudeOnly, "claude", false, "install only the Claude Code hooks")
	cmd.Flags().BoolVar(&codexOnly, "codex", false, "install only the Codex hooks")
	cmd.Flags().StringVar(&binary, "binary", "", "binary path to record (default: this binary)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report changes without writing")
	cmd.Flags().BoolVar(&noSandbox, "no-sandbox-config", false, "skip the Codex sandbox grant")
	return cmd
}

// mergeHooks applies this tool's entries to a hook map. Every managed event is
// visited, not just the tracked ones, so an event this tool has stopped
// installing is removed rather than orphaned. Other hooks are left alone.
func mergeHooks(hooks map[string]any, tracked []eventSpec, exe, harness string) map[string]any {
	timeouts := map[string]int{}
	for _, event := range tracked {
		timeouts[event.name] = event.timeout
	}
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

// installSkill writes the room skill, and never over something you changed.
//
// Overwriting a hand-edited skill is the obvious way to get this wrong: the
// skill is meant to be tuned once you see what agents actually write. A hash of
// the last version this tool wrote distinguishes its own output from yours.
func installSkill(path string, dryRun bool) (skillOutcome, error) {
	existing, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if dryRun {
			return skillWritten, nil
		}
		return skillWritten, writeSkill(path)
	case err != nil:
		return skillUnchanged, fmt.Errorf("read %s: %w", path, err)
	}

	if bytes.Equal(existing, skillDoc) {
		// Content is ours even if no marker was recorded - an install from
		// before the marker existed. Record it, or the next change would be
		// mistaken for a local edit.
		if !dryRun && !ours(path, existing) {
			if err := writeMarker(path, existing); err != nil {
				return skillUnchanged, err
			}
		}
		return skillUnchanged, nil
	}
	if !ours(path, existing) {
		if dryRun {
			return skillPreserved, nil
		}
		return skillPreserved, os.WriteFile(path+".new", skillDoc, 0o644)
	}
	if dryRun {
		return skillWritten, nil
	}
	return skillWritten, writeSkill(path)
}

// ours reports whether content is exactly what this tool last wrote here.
func ours(path string, content []byte) bool {
	recorded, _ := readMarker(path)
	return recorded == digest(content)
}

// readMarker returns the hash and the version that wrote it.
func readMarker(path string) (hash, version string) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), installedMarker))
	if err != nil {
		return "", ""
	}
	lines := strings.Fields(string(data))
	if len(lines) == 0 {
		return "", ""
	}
	if len(lines) == 1 {
		return lines[0], "unknown"
	}
	return lines[0], lines[1]
}

func writeSkill(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	if err := os.WriteFile(path, skillDoc, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return writeMarker(path, skillDoc)
}

// writeMarker records the hash and the version that wrote it, so a later
// install can say what it is replacing without guessing.
func writeMarker(path string, content []byte) error {
	marker := filepath.Join(filepath.Dir(path), installedMarker)
	return os.WriteFile(marker, []byte(digest(content)+"\n"+Version()+"\n"), 0o644)
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// replacing names the version being replaced, when it was not this one.
func replacing(previous string) string {
	if previous == "" || previous == "unknown" || previous == Version() {
		return ""
	}
	return " (was " + previous + ")"
}

// transient reports whether a binary path will not exist later. Running
// "go run ./cmd/multiplayer install" would otherwise record a build-cache path
// that is deleted on exit, leaving hooks that fail silently forever.
func transient(path string) bool {
	return strings.Contains(path, "/go-build") || strings.HasPrefix(path, os.TempDir())
}

func filterTargets(name string) []target {
	for _, t := range targets {
		if t.name == name {
			return []target{t}
		}
	}
	return nil
}

// installInto merges the registry's hook entries into path, reporting whether
// the file needed changing.
func installInto(path string, t target, exe string, dryRun bool) (bool, error) {
	original, config, err := readConfig(path)
	if err != nil {
		return false, err
	}

	config[t.root] = mergeHooks(mapAt(config, t.root), trackedEvents, exe, t.harness)

	updated, err := encodeConfig(config)
	if err != nil {
		return false, err
	}

	if string(original) == string(updated) {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if err := backup(path, original); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func encodeConfig(config map[string]any) ([]byte, error) {
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return append(out, '\n'), nil
}

// readConfig loads path, treating a missing file as an empty object. The raw
// bytes come back too so an unchanged file can be left alone.
func readConfig(path string) ([]byte, map[string]any, error) {
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

// backup copies the previous contents aside before the file is rewritten.
func backup(path string, original []byte) error {
	if len(original) == 0 {
		return nil
	}
	dst := fmt.Sprintf("%s.bak-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(dst, original, 0o644); err != nil {
		return fmt.Errorf("back up %s: %w", path, err)
	}
	return nil
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

// mapAt returns the nested object at key, creating it when absent.
func mapAt(config map[string]any, key string) map[string]any {
	if existing, ok := config[key].(map[string]any); ok {
		return existing
	}
	return map[string]any{}
}
