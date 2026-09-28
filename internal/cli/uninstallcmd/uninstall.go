// Package uninstallcmd removes what init added.
package uninstallcmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/backup"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/hookconfig"
	"github.com/codyhartsook/multiplayer/internal/cli/skill"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/codex"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		confirm bool
		purge   bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the hooks, skill and sandbox grant init added",
		Long: `Removes only what init added: its own hook entries, the room skill, role
skills wherever crew wrote them, and Codex's sandbox grant. Other hooks and
settings are left alone. Git exclude lines stay.

The store is your data and is kept unless you pass --purge. Without --yes
nothing is removed; what would go is listed instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			out := cmd.OutOrStdout()
			dryRun := !confirm

			for _, t := range harness.Specs() {
				if err := uninstallFrom(cmd, home, t, dryRun); err != nil {
					return fmt.Errorf("%s: %w", t.Harness, err)
				}
			}

			if err := removeRoomRoles(cmd, opts, dryRun); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "crew: room role skills:", err)
			}

			if purge {
				dir, err := opts.StoreDir()
				if err != nil {
					return err
				}
				if _, err := os.Stat(dir); err == nil {
					if dryRun {
						fmt.Fprintf(out, "would delete the store at %s\n", dir)
					} else if err := os.RemoveAll(dir); err != nil {
						return fmt.Errorf("remove store: %w", err)
					} else {
						fmt.Fprintf(out, "deleted the store at %s\n", dir)
					}
				}
			}

			if dryRun {
				fmt.Fprintln(out, "\nnothing removed; re-run with --yes")
			} else if !purge {
				dir, _ := opts.StoreDir()
				fmt.Fprintf(out, "\nthe store is still at %s; --purge removes it too\n", dir)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "yes", false, "actually remove; without it the changes are only listed")
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the store and its hook log")
	return cmd
}

func uninstallFrom(cmd *cobra.Command, home string, t harness.Spec, dryRun bool) error {
	out := cmd.OutOrStdout()
	name := string(t.Harness)
	if t.Harness == session.HarnessClaude {
		if _, err := hookconfig.UnregisterLegacyChannel(filepath.Join(home, ".claude.json"), dryRun); err != nil {
			return err
		}
	}

	path := filepath.Join(home, t.ConfigPath)
	original, config, err := hookconfig.Read(path)
	if err != nil {
		return err
	}
	if len(original) > 0 {
		// An empty tracked set makes the same merge that installs also
		// uninstall: every managed event is visited and this tool's entries
		// swept.
		hooks := hookconfig.Merge(hookconfig.MapAt(config, t.ConfigRoot), nil, "", name)
		if len(hooks) == 0 {
			delete(config, t.ConfigRoot)
		} else {
			config[t.ConfigRoot] = hooks
		}

		// A file that held nothing but our hooks is ours to remove; one with
		// anything else in it is rewritten without them.
		empty := len(config) == 0
		updated, err := hookconfig.Encode(config)
		if err != nil {
			return err
		}
		if empty || string(original) != string(updated) {
			switch {
			case dryRun && empty:
				fmt.Fprintf(out, "%s: would delete %s\n", name, path)
			case dryRun:
				fmt.Fprintf(out, "%s: would remove hooks from %s\n", name, path)
			default:
				if err := backup.Save(path, original); err != nil {
					return err
				}
				if empty {
					if err := os.Remove(path); err != nil {
						return fmt.Errorf("remove %s: %w", path, err)
					}
					fmt.Fprintf(out, "%s: deleted %s\n", name, path)
					break
				}
				if err := os.WriteFile(path, updated, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", path, err)
				}
				fmt.Fprintf(out, "%s: hooks removed from %s\n", name, path)
			}
		}
	}

	skillDir := filepath.Join(t.SkillsPath(home), skill.Name)
	if err := removeSkill(cmd, name, skillDir, dryRun); err != nil {
		return err
	}
	if _, err := skill.RemoveLegacy(t.SkillsPath(home), dryRun); err != nil {
		return err
	}
	roles, err := skill.RemoveRoles(t.SkillsPath(home), dryRun)
	if err != nil {
		return err
	}
	if roles > 0 {
		verb := "role skills removed"
		if dryRun {
			verb = "role skills to remove"
		}
		fmt.Fprintf(out, "%s: %s: %d\n", name, verb, roles)
	}

	if t.SandboxTOML == "" {
		return nil
	}
	// The store directory is the grant; resolve it the same way init did.
	storeDir, err := (&cmdutil.Options{}).StoreDir()
	if err != nil {
		return err
	}
	sandboxPath := filepath.Join(home, t.SandboxTOML)
	removed, err := codex.RemoveWritableRoot(sandboxPath, storeDir, dryRun)
	if err != nil {
		return err
	}
	if removed {
		verb := "sandbox grant removed from"
		if dryRun {
			verb = "would remove sandbox grant from"
		}
		fmt.Fprintf(out, "%s: %s %s\n", name, verb, sandboxPath)
	}
	return nil
}

// removeRoomRoles clears role skills from every directory sync wrote to.
func removeRoomRoles(cmd *cobra.Command, opts *cmdutil.Options, dryRun bool) error {
	path, err := opts.DBPath()
	if err != nil || opts.Server != "" {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	st, err := opts.OpenStore()
	if err != nil {
		return err
	}
	defer st.Close()
	rs, ok := st.(store.RoleStore)
	if !ok {
		return nil
	}
	ctx := cmd.Context()
	dirs, err := rs.SkillDirs(ctx)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		n, err := skill.RemoveRoles(dir, dryRun)
		if err != nil {
			return err
		}
		if n > 0 {
			verb := "role skills removed"
			if dryRun {
				verb = "role skills to remove"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s: %d\n", dir, verb, n)
		}
		if !dryRun {
			if err := rs.ForgetSkillDir(ctx, dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeSkill deletes the skill only when it is untouched. A skill you edited
// is yours, and uninstalling the tool is no reason to throw the edit away.
func removeSkill(cmd *cobra.Command, name, dir string, dryRun bool) error {
	path := filepath.Join(dir, "SKILL.md")
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	out := cmd.OutOrStdout()
	if !skill.Ours(path, content) {
		fmt.Fprintf(out, "%s: skill has local edits, kept at %s\n", name, path)
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "%s: would remove skill from %s\n", name, dir)
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	fmt.Fprintf(out, "%s: skill removed from %s\n", name, dir)
	return nil
}
