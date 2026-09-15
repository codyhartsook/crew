// Package rolecmd lists the role identities crew's delegation system reads,
// and activates or deactivates them for a room.
package rolecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/cli/view"
	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "roles",
		Short: "List defined role identities",
		Long: `Reads role definitions from .crew/agents/*.toml in the repository and
~/.crew/agents/*.toml globally. A repo definition wins on a name collision,
since it is version-controlled and reviewable.

A discovered role is dormant until this location's room activates it, with
"roles activate". Active here marks which ones already are.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := discover(cmd.Context())
			if err != nil {
				return err
			}
			active := activeHere(cmd.Context(), opts)
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), reg.All())
			}
			return writeTable(cmd.OutOrStdout(), reg, active)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.AddCommand(newActivate(opts), newDeactivate(opts))
	return cmd
}

// discover resolves the current directory into a repo root, if any, and reads
// both scopes.
func discover(ctx context.Context) (*role.Registry, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	return role.DiscoverFromDir(ctx, cwd)
}

// activeHere is which roles this location's room has activated. Best effort:
// a listing outside any room, or against a store that predates activation,
// degrades to an empty set rather than failing the whole command.
func activeHere(ctx context.Context, opts *cmdutil.Options) map[string]bool {
	rc, err := roomctx.Open(ctx, opts, roomctx.Cwd())
	if err != nil {
		return nil
	}
	defer rc.Close()
	rs, ok := rc.Store.(store.RoleStore)
	if !ok {
		return nil
	}
	out := map[string]bool{}
	for _, r := range rc.Here {
		activations, err := rs.ActiveRoles(ctx, r.Key)
		if err != nil {
			continue
		}
		for _, a := range activations {
			out[a.Role] = true
		}
	}
	return out
}

var roleColumns = []view.Column{
	{Name: "NAME"},
	{Name: "SCOPE"},
	{Name: "ACTIVE"},
	{Name: "HARNESS"},
	{Name: "DESCRIPTION"},
}

func writeTable(w io.Writer, reg *role.Registry, active map[string]bool) error {
	defs := reg.All()
	if len(defs) == 0 {
		_, err := fmt.Fprintln(w, "no roles defined")
		return err
	}
	rows := make([][]string, 0, len(defs))
	for _, d := range defs {
		mark := "-"
		if active[d.Name] {
			mark = "yes"
		}
		rows = append(rows, []string{d.Name, string(d.Scope), mark, string(d.Harness), d.Description})
	}
	if err := view.Table(w, true, roleColumns, rows); err != nil {
		return err
	}
	for _, d := range defs {
		if shadowed, ok := reg.Shadowed(d.Name); ok {
			fmt.Fprintf(w, "\n%q also defined globally at %s; the repo definition wins.\n", d.Name, shadowed.Path)
		}
	}
	return nil
}

func writeJSON(w io.Writer, defs []role.Definition) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(defs)
}
