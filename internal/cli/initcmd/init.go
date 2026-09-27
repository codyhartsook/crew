// Package initcmd sets a harness up to talk to crew and runs the local
// broker.
package initcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/backup"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/dashboardcmd"
	"github.com/codyhartsook/multiplayer/internal/cli/hookconfig"
	"github.com/codyhartsook/multiplayer/internal/cli/server"
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
		headless   bool
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
			storeDir, err := opts.StoreDir()
			if err != nil {
				return err
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
			rep := newReport(out, verbose)
			showDashboard := func(url string) {
				err := maybeOpenDashboard(cmd.Context(), url, headless)
				if headless {
					return
				}
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "crew: open dashboard: %v\n", err)
					return
				}
				fmt.Fprintln(out, "  "+rep.view.muted("dashboard opened"))
			}

			fmt.Fprintln(out, rep.view.heading("Setup"))
			for _, spec := range selected {
				if err := rep.install(cmd.Context(), install{
					spec:     spec,
					home:     home,
					exe:      exe,
					storeDir: storeDir,
					dryRun:   dryRun,
					sandbox:  !noSandbox,
				}); err != nil {
					return err
				}
			}
			if dryRun {
				return nil
			}

			fmt.Fprintln(out)
			baseURL := "http://" + addr
			// Someone else is already serving, so this command has nothing
			// left to do. Say that plainly: the same tick that means "now
			// serving" below would otherwise read as if init stayed up.
			if server.IsUp(cmd.Context(), baseURL) {
				fmt.Fprintf(out, "  %s broker already running at %s\n", rep.view.success("✓"), baseURL)
				fmt.Fprintln(out, "  "+rep.view.muted("started by another process, so init is exiting rather than serving"))
				fmt.Fprintln(out, "  "+rep.view.muted("stop the foreground `crew init` with Ctrl-C before starting another"))
				showDashboard(baseURL)
				printInitNextSteps(out, rep.view, headless)
				return nil
			}
			return server.Serve(cmd.Context(), opts, server.Config{
				Addr:    addr,
				Verbose: verbose,
				Log:     cmd.ErrOrStderr(),
				OnReady: func(url string) {
					fmt.Fprintf(out, "  %s broker running at %s (notifications enabled)\n", rep.view.success("✓"), url)
					fmt.Fprintln(out, "  "+rep.view.muted("serving here; this command stays running until you stop it"))
					showDashboard(url)
					printInitNextSteps(out, rep.view, headless)
				},
			})
		},
	}

	cmd.Flags().BoolVar(&claudeOnly, "claude", false, "initialize only Claude Code")
	cmd.Flags().BoolVar(&codexOnly, "codex", false, "initialize only Codex")
	cmd.Flags().StringVar(&binary, "binary", "", "binary path to record (default: this binary)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report changes without writing")
	cmd.Flags().BoolVar(&noSandbox, "no-sandbox-config", false, "skip the Codex sandbox grant")
	cmd.Flags().StringVar(&addr, "addr", server.DefaultAddr, "address for the local broker")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "list every setup step and log every broker request")
	cmd.Flags().BoolVar(&headless, "headless", false, "do not open the dashboard")
	return cmd
}

// install is one harness's setup.
type install struct {
	spec     harness.Spec
	home     string
	exe      string
	storeDir string
	dryRun   bool
	sandbox  bool
}

// install writes one harness's hooks, skill, and sandbox grant, reporting
// each as it lands.
func (r *report) install(ctx context.Context, in install) error {
	spec := in.spec
	name := string(spec.Harness)
	configPath := filepath.Join(in.home, spec.ConfigPath)
	r.section(spec.Label, tilde(in.home, configPath))

	hooks, changed, err := applyHooks(configPath, spec, in.exe, in.dryRun)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for _, hook := range hooks {
		r.step(ctx, hook.Event+" hook", state(in.dryRun, !hook.Current),
			fmt.Sprintf("%ds timeout", hook.Timeout))
	}

	if spec.Harness == session.HarnessClaude {
		removed, err := hookconfig.UnregisterLegacyChannel(filepath.Join(in.home, ".claude.json"), in.dryRun)
		if err != nil {
			return fmt.Errorf("%s channel cleanup: %w", name, err)
		}
		if removed {
			r.step(ctx, "legacy channel entry", state(in.dryRun, true), tilde(in.home, filepath.Join(in.home, ".claude.json")))
			changed = true
		}
	}

	var granted bool
	if spec.SandboxTOML != "" && in.sandbox {
		sandboxPath := filepath.Join(in.home, spec.SandboxTOML)
		granted, err = codex.EnsureWritableRoot(sandboxPath, in.storeDir, in.dryRun)
		if err != nil {
			return fmt.Errorf("%s sandbox: %w", name, err)
		}
		r.step(ctx, "sandbox write access", state(in.dryRun, granted), tilde(in.home, in.storeDir))
	}

	skillPath := filepath.Join(spec.SkillsPath(in.home), skill.Name, "SKILL.md")
	outcome, err := skill.Install(skillPath, skill.Doc(), in.dryRun)
	if err != nil {
		return fmt.Errorf("%s skill: %w", name, err)
	}
	if outcome == skill.Preserved {
		r.step(ctx, "room skill", stepWarn, "local edits kept; new version at "+tilde(in.home, skillPath)+".new")
	} else {
		r.step(ctx, "room skill", state(in.dryRun, outcome == skill.Written), tilde(in.home, filepath.Dir(skillPath)))
	}

	sweptLegacy, err := skill.RemoveLegacy(spec.SkillsPath(in.home), in.dryRun)
	if err != nil {
		return fmt.Errorf("%s skill: %w", name, err)
	}
	if sweptLegacy {
		r.step(ctx, "legacy room skill", state(in.dryRun, true), "swept")
	}

	r.flush(ctx)

	summary := "ready"
	if changed || outcome == skill.Written || granted || sweptLegacy {
		summary = "configured"
		if in.dryRun {
			summary = "would be configured"
		}
	}
	fmt.Fprintf(r.out, "  %s %s integration %s\n", r.view.success("✓"), name, summary)
	return nil
}

func printInitNextSteps(out io.Writer, view initView, headless bool) {
	fmt.Fprintln(out, "\n"+view.heading("Next"))
	fmt.Fprintln(out, "  1. Start new agent sessions so they pick up the hooks.")
	if headless {
		fmt.Fprintln(out, "  2. Open the dashboard: crew dashboard")
	}
	fmt.Fprintln(out, "\n"+view.muted("Codex asks to trust a newly added hook the first time it runs."))
}

var openDashboard = dashboardcmd.OpenDashboard

func maybeOpenDashboard(ctx context.Context, url string, headless bool) error {
	if headless {
		return nil
	}
	return openDashboard(ctx, url, false)
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

// hookStatus is one lifecycle hook as init found it, before writing.
type hookStatus struct {
	Event   string
	Timeout int
	Current bool
}

// applyHooks merges our hook entries into path, reporting every hook the
// harness prices, in firing order, and whether the file needed changing.
func applyHooks(path string, t harness.Spec, exe string, dryRun bool) ([]hookStatus, bool, error) {
	original, config, err := hookconfig.Read(path)
	if err != nil {
		return nil, false, err
	}
	hooks := hookconfig.MapAt(config, t.ConfigRoot)

	var installed []hookStatus
	for _, event := range hookconfig.Events() {
		timeout, priced := t.Timeouts[event]
		if !priced {
			continue
		}
		installed = append(installed, hookStatus{
			Event:   event,
			Timeout: timeout,
			Current: hookconfig.Installed(hooks, event, exe, string(t.Harness), timeout),
		})
	}

	config[t.ConfigRoot] = hookconfig.Merge(hooks, t.Timeouts, exe, string(t.Harness))
	updated, err := hookconfig.Encode(config)
	if err != nil {
		return nil, false, err
	}

	if string(original) == string(updated) {
		return installed, false, nil
	}
	if dryRun {
		return installed, true, nil
	}
	if err := backup.Save(path, original); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return nil, false, fmt.Errorf("write %s: %w", path, err)
	}
	return installed, true, nil
}

// tilde shortens a path under home so report lines stay narrow.
func tilde(home, path string) string {
	if rest, ok := strings.CutPrefix(path, home+string(os.PathSeparator)); ok {
		return "~" + string(os.PathSeparator) + rest
	}
	return path
}
