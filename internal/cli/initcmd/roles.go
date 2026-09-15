package initcmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// reviewRoles offers to activate discovered roles; it fails soft throughout,
// since a role never blocks the hooks init exists to install.
func reviewRoles(ctx context.Context, opts *cmdutil.Options, in io.Reader, out io.Writer, view initView, headless bool) {
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

	for _, name := range promptActivation(in, out, view, reg.All(), active) {
		_ = rs.ActivateRole(ctx, &role.Activation{Room: target.Key, Role: name, ActivatedAt: time.Now().UTC()})
	}
}

// promptActivation asks which inactive roles to turn on; pure enough aside
// from the read/print to test without a real terminal.
func promptActivation(in io.Reader, out io.Writer, view initView, defs []role.Definition, active map[string]bool) []string {
	var candidates []role.Definition
	for _, d := range defs {
		if !active[d.Name] {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	fmt.Fprintln(out, "\n"+view.heading("Roles"))
	for _, d := range candidates {
		fmt.Fprintf(out, "  %s %s (%s) %s\n", view.muted("·"), d.Name, d.Harness, d.Description)
	}
	fmt.Fprint(out, "  activate which here? [names, comma separated, \"all\", or Enter to skip]: ")

	line, _ := bufio.NewReader(in).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if strings.EqualFold(line, "all") {
		names := make([]string, len(candidates))
		for i, d := range candidates {
			names[i] = d.Name
		}
		return names
	}
	known := map[string]bool{}
	for _, d := range candidates {
		known[d.Name] = true
	}
	var chosen []string
	for _, part := range strings.Split(line, ",") {
		if name := strings.TrimSpace(part); known[name] {
			chosen = append(chosen, name)
		}
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
