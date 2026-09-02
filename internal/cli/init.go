package cli

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/backup"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/codex"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// marker identifies hook entries this tool owns. It matches the flag
// combination rather than the binary path, which is quoted and may be
// reinstalled from elsewhere.
const marker = "hook --harness "

// managedEvents is every event this tool has ever installed. An event a harness
// no longer prices in its Timeouts is swept on init, so dropping one removes
// it rather than leaving an orphan hook firing with no way to uninstall it.
var managedEvents = []string{"SessionStart", "UserPromptSubmit", "SessionEnd"}

// initMarker records the hash of the skill this tool last wrote, so a
// re-init can tell its own previous output from something you edited.
const initMarker = ".installed"

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

func newInitCmd(opts *options) *cobra.Command {
	var (
		claudeOnly bool
		codexOnly  bool
		binary     string
		dryRun     bool
		noSandbox  bool
		addr       string
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up agent integration and run the notification broker",
		Long: `Adds the session hooks pointing at this binary, writes the room skill,
and grants Codex's sandbox write access to the store directory, without which
every room write from a Codex agent fails as a readonly database.

It is safe to run again: our entries are reconciled, others are untouched, and
the broker wakes live sessions when they have addressed entries.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.server != "" {
				return fmt.Errorf("init starts a local broker; unset --server")
			}
			exe := binary
			if exe == "" {
				resolved, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolve this binary's path: %w", err)
				}
				if transient(resolved) {
					return fmt.Errorf("this binary is temporary (%s) and the hooks would point at a path that stops existing.\nInstall it first:\n  make install\nThen run:\n  multiplayer init\nor pass --binary <path>", resolved)
				}
				exe = resolved
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}

			selected := harness.Specs()
			if claudeOnly != codexOnly {
				only := session.HarnessClaude
				if codexOnly {
					only = session.HarnessCodex
				}
				selected = filterSpecs(only)
			}

			out := cmd.OutOrStdout()
			view := newInitView(out)
			fmt.Fprintln(out, view.heading("Setup"))
			for _, t := range selected {
				name := string(t.Harness)
				var granted bool
				path := filepath.Join(home, t.ConfigPath)
				changed, err := initInto(path, t, exe, dryRun)
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				if t.Harness == session.HarnessClaude {
					removed, err := unregisterLegacyChannel(filepath.Join(home, ".claude.json"), dryRun)
					if err != nil {
						return fmt.Errorf("%s channel cleanup: %w", name, err)
					}
					changed = changed || removed
				}
				if t.SandboxTOML != "" && !noSandbox {
					storeDir, err := opts.storeDir()
					if err != nil {
						return err
					}
					sandboxPath := filepath.Join(home, t.SandboxTOML)
					granted, err = codex.EnsureWritableRoot(sandboxPath, storeDir, dryRun)
					if err != nil {
						return fmt.Errorf("%s sandbox: %w", name, err)
					}
				}
				skillPath := filepath.Join(home, t.SkillsDir, skillName, "SKILL.md")
				outcome, err := initSkill(skillPath, dryRun)
				if err != nil {
					return fmt.Errorf("%s skill: %w", name, err)
				}
				if outcome == skillPreserved {
					fmt.Fprintf(out, "  %s %s room skill has local edits; new version at %s.new\n", view.warning("!"), name, skillPath)
				}
				state := "ready"
				if changed || outcome == skillWritten || granted {
					state = "configured"
					if dryRun {
						state = "would be configured"
					}
				}
				fmt.Fprintf(out, "  %s %s integration %s\n", view.success("✓"), name, state)
			}
			if !dryRun {
				baseURL := "http://" + addr
				if registryIsUp(cmd.Context(), baseURL) {
					fmt.Fprintf(out, "  %s broker already running at %s\n", view.success("✓"), baseURL)
					printInitNextSteps(out, view)
					return nil
				}
				return serveRegistry(cmd, opts, addr, verbose, func(url string) {
					fmt.Fprintf(out, "  %s broker running at %s (notifications enabled)\n", view.success("✓"), url)
					printInitNextSteps(out, view)
				})
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&claudeOnly, "claude", false, "initialize only Claude Code")
	cmd.Flags().BoolVar(&codexOnly, "codex", false, "initialize only Codex")
	cmd.Flags().StringVar(&binary, "binary", "", "binary path to record (default: this binary)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report changes without writing")
	cmd.Flags().BoolVar(&noSandbox, "no-sandbox-config", false, "skip the Codex sandbox grant")
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address for the local broker")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log every broker request")
	return cmd
}

func printInitNextSteps(out io.Writer, view initView) {
	fmt.Fprintln(out, "\n"+view.heading("Next"))
	fmt.Fprintln(out, "  1. Start new agent sessions so they pick up the hooks.")
	fmt.Fprintln(out, "  2. Open the dashboard: multiplayer dashboard")
	fmt.Fprintln(out, "\n"+view.muted("Codex asks to trust a newly added hook the first time it runs."))
}

type initView struct{ color bool }

func newInitView(out io.Writer) initView {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return initView{}
	}
	file, ok := out.(*os.File)
	if !ok {
		return initView{}
	}
	info, err := file.Stat()
	return initView{color: err == nil && info.Mode()&os.ModeCharDevice != 0}
}

func (v initView) heading(text string) string { return v.style("1;36", text) }
func (v initView) success(text string) string { return v.style("32", text) }
func (v initView) warning(text string) string { return v.style("33", text) }
func (v initView) muted(text string) string   { return v.style("2", text) }

func (v initView) style(code, text string) string {
	if !v.color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// mergeHooks applies this tool's entries to a hook map. Every managed event is
// visited, not just the tracked ones, so an event this tool has stopped
// installing is removed rather than orphaned. Other hooks are left alone.
func mergeHooks(hooks map[string]any, timeouts map[string]int, exe, harness string) map[string]any {
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

// initSkill writes the room skill, and never over something you changed.
//
// Overwriting a hand-edited skill is the obvious way to get this wrong: the
// skill is meant to be tuned once you see what agents actually write. A hash of
// the last version this tool wrote distinguishes its own output from yours.
func initSkill(path string, dryRun bool) (skillOutcome, error) {
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
		// Content is ours even if no marker was recorded - an init from
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
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), initMarker))
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
// init can say what it is replacing without guessing.
func writeMarker(path string, content []byte) error {
	marker := filepath.Join(filepath.Dir(path), initMarker)
	return os.WriteFile(marker, []byte(digest(content)+"\n"+Version()+"\n"), 0o644)
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// transient reports whether a binary path will not exist later. Running
// "go run ./cmd/multiplayer init" would otherwise record a build-cache path
// that is deleted on exit, leaving hooks that fail silently forever.
func transient(path string) bool {
	return strings.Contains(path, "/go-build") || strings.HasPrefix(path, os.TempDir())
}

func filterSpecs(only session.Harness) []harness.Spec {
	for _, t := range harness.Specs() {
		if t.Harness == only {
			return []harness.Spec{t}
		}
	}
	return nil
}

// initInto merges the registry's hook entries into path, reporting whether
// the file needed changing.
func initInto(path string, t harness.Spec, exe string, dryRun bool) (bool, error) {
	original, config, err := readConfig(path)
	if err != nil {
		return false, err
	}

	config[t.ConfigRoot] = mergeHooks(mapAt(config, t.ConfigRoot), t.Timeouts, exe, string(t.Harness))

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
	if err := backup.Save(path, original); err != nil {
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
