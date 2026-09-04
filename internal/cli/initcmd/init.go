// Package initcmd sets a harness up to talk to crew and runs the local
// broker.
package initcmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/backup"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/hookconfig"
	"github.com/codyhartsook/multiplayer/internal/cli/registry"
	"github.com/codyhartsook/multiplayer/internal/cli/skill"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/codex"
	"github.com/codyhartsook/multiplayer/internal/session"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		claudeOnly bool
		codexOnly  bool
		binary     string
		dryRun     bool
		noSandbox  bool
		addr       string
		verbose    bool
		restart    bool
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
			if dryRun && restart {
				return fmt.Errorf("--dry-run and --restart cannot be used together")
			}
			if opts.Server != "" {
				return fmt.Errorf("init starts a local broker; unset --server")
			}
			exe := binary
			if exe == "" {
				resolved, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolve this binary's path: %w", err)
				}
				if transient(resolved) {
					return fmt.Errorf("this binary is temporary (%s) and the hooks would point at a path that stops existing.\nInstall it first:\n  make install\nThen run:\n  crew init\nor pass --binary <path>", resolved)
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
					removed, err := hookconfig.UnregisterLegacyChannel(filepath.Join(home, ".claude.json"), dryRun)
					if err != nil {
						return fmt.Errorf("%s channel cleanup: %w", name, err)
					}
					changed = changed || removed
				}
				if t.SandboxTOML != "" && !noSandbox {
					storeDir, err := opts.StoreDir()
					if err != nil {
						return err
					}
					sandboxPath := filepath.Join(home, t.SandboxTOML)
					granted, err = codex.EnsureWritableRoot(sandboxPath, storeDir, dryRun)
					if err != nil {
						return fmt.Errorf("%s sandbox: %w", name, err)
					}
				}
				skillPath := filepath.Join(home, t.SkillsDir, skill.Name, "SKILL.md")
				outcome, err := skill.Install(skillPath, dryRun)
				if err != nil {
					return fmt.Errorf("%s skill: %w", name, err)
				}
				sweptLegacy, err := skill.RemoveLegacy(filepath.Join(home, t.SkillsDir), dryRun)
				if err != nil {
					return fmt.Errorf("%s skill: %w", name, err)
				}
				if outcome == skill.Preserved {
					fmt.Fprintf(out, "  %s %s room skill has local edits; new version at %s.new\n", view.warning("!"), name, skillPath)
				}
				state := "ready"
				if changed || outcome == skill.Written || granted || sweptLegacy {
					state = "configured"
					if dryRun {
						state = "would be configured"
					}
				}
				fmt.Fprintf(out, "  %s %s integration %s\n", view.success("✓"), name, state)
			}
			if !dryRun {
				baseURL := "http://" + addr
				if restart && registry.IsUp(cmd.Context(), baseURL) {
					fmt.Fprintf(out, "  %s stopping broker at %s\n", view.success("✓"), baseURL)
					if err := registry.Stop(cmd.Context(), baseURL); err != nil {
						return err
					}
				}
				// Someone else is already serving, so this command has nothing
				// left to do. Say that plainly: the same tick that means "now
				// serving" below would otherwise read as if init stayed up.
				if registry.IsUp(cmd.Context(), baseURL) {
					fmt.Fprintf(out, "  %s broker already running at %s\n", view.success("✓"), baseURL)
					fmt.Fprintln(out, "  "+view.muted("started by another process, so init is exiting rather than serving"))
					fmt.Fprintln(out, "  "+view.muted("run `crew init --restart` to replace it with this binary"))
					printInitNextSteps(out, view)
					return nil
				}
				return registry.Serve(cmd.Context(), opts, registry.Config{
					Addr:    addr,
					Verbose: verbose,
					Log:     cmd.ErrOrStderr(),
					OnReady: func(url string) {
						fmt.Fprintf(out, "  %s broker running at %s (notifications enabled)\n", view.success("✓"), url)
						fmt.Fprintln(out, "  "+view.muted("serving here; this command stays running until you stop it"))
						printInitNextSteps(out, view)
					},
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
	cmd.Flags().StringVar(&addr, "addr", registry.DefaultAddr, "address for the local broker")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log every broker request")
	cmd.Flags().BoolVar(&restart, "restart", false, "gracefully replace a running broker")
	return cmd
}

func printInitNextSteps(out io.Writer, view initView) {
	fmt.Fprintln(out, "\n"+view.heading("Next"))
	fmt.Fprintln(out, "  1. Start new agent sessions so they pick up the hooks.")
	fmt.Fprintln(out, "  2. Open the dashboard: crew dashboard")
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

// transient reports whether a binary path will not exist later. Running
// "go run ./cmd/crew init" would otherwise record a build-cache path
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
	original, config, err := hookconfig.Read(path)
	if err != nil {
		return false, err
	}

	config[t.ConfigRoot] = hookconfig.Merge(hookconfig.MapAt(config, t.ConfigRoot), t.Timeouts, exe, string(t.Harness))

	updated, err := hookconfig.Encode(config)
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
