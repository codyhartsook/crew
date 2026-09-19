package initcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// reviewRoles offers to activate discovered roles; it fails soft throughout,
// since a role never blocks the hooks init exists to install.
func reviewRoles(ctx context.Context, opts *cmdutil.Options, in io.Reader, out io.Writer, headless bool) {
	if headless || !isTerminalReader(in) {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	place, err := detect.New().Detect(ctx, cwd)
	if err != nil || place == nil {
		return
	}
	here := room.For(*place)
	if len(here) == 0 {
		return
	}
	target := here[0]

	repoRoot := ""
	if place.Repo != nil {
		repoRoot = place.Repo.Root
	}
	reg, err := role.DiscoverFor(repoRoot)
	if err != nil || len(reg.All()) == 0 {
		return
	}

	st, err := opts.OpenStore()
	if err != nil {
		return
	}
	defer st.Close()
	rs, ok := st.(store.RoleStore)
	if !ok {
		return
	}
	activeNow, err := rs.ActiveRoles(ctx, target.Key)
	if err != nil {
		return
	}
	active := map[string]bool{}
	for _, a := range activeNow {
		active[a.Role] = true
	}

	for _, name := range promptActivation(in, out, reg.All(), active) {
		_ = rs.ActivateRole(ctx, &role.Activation{Room: target.Key, Role: name, ActivatedAt: time.Now().UTC()})
	}
}

// inactiveRoles is defs minus whatever active already lists; nothing left to
// offer once every discovered role is already on, so asking would be noise.
func inactiveRoles(defs []role.Definition, active map[string]bool) []role.Definition {
	var out []role.Definition
	for _, d := range defs {
		if !active[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

// promptActivation is an interactive checklist of the inactive roles; esc or
// ctrl-c cancels the same as an empty selection, since a skipped role never
// blocks the hooks init exists to install.
func promptActivation(in io.Reader, out io.Writer, defs []role.Definition, active map[string]bool) []string {
	candidates := inactiveRoles(defs, active)
	if len(candidates) == 0 {
		return nil
	}

	options := make([]huh.Option[string], len(candidates))
	for i, d := range candidates {
		options[i] = huh.NewOption(fmt.Sprintf("%s (%s) %s", d.Name, d.Harness, d.Description), d.Name)
	}

	var chosen []string
	// No Description: huh renders its own key help below the list, and a second
	// hand-written one only contradicts it.
	field := huh.NewMultiSelect[string]().
		Title("Roles").
		Options(options...).
		Value(&chosen)

	form := huh.NewForm(huh.NewGroup(field)).WithInput(in).WithOutput(out).WithTheme(huh.ThemeCharm())
	if err := form.Run(); err != nil {
		return nil
	}
	return chosen
}

// isTerminalReader reports whether in is a real terminal, so a piped or
// scripted stdin skips the prompt instead of blocking forever.
func isTerminalReader(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
