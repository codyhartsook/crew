package initcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Setup finishes in milliseconds, so steps are held on screen long enough to
// read. Only for a terminal: piped output skips the wait.
const (
	stepPause  = 220 * time.Millisecond
	framePace  = 80 * time.Millisecond
	labelWidth = 22
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// stepState is how a step turned out, and picks its bullet.
type stepState int

const (
	stepDone    stepState = iota // changed something
	stepCurrent                  // already in place
	stepPlanned                  // --dry-run
	stepWarn
)

// state keeps --dry-run honest: pending rather than done.
func state(dryRun, changed bool) stepState {
	switch {
	case !changed:
		return stepCurrent
	case dryRun:
		return stepPlanned
	default:
		return stepDone
	}
}

// report renders the checklist: a section per harness, a step per hook.
//
// Steps are buffered rather than printed as they land. Re-running init is the
// common case and changes nothing, so a run that reports every unchanged item
// spends the reader's attention on news that is not there.
type report struct {
	out     io.Writer
	view    initView
	verbose bool
	pending []pendingStep
}

// pendingStep is a step held back until the section knows what to show.
type pendingStep struct {
	label  string
	state  stepState
	detail string
}

func newReport(out io.Writer, verbose bool) *report {
	return &report{out: out, view: newInitView(out), verbose: verbose}
}

// section opens a harness block with the file its hooks are written into.
func (r *report) section(name, path string) {
	fmt.Fprintf(r.out, "\n  %s  %s\n", r.view.name(name), r.view.muted(path))
}

func (r *report) step(_ context.Context, label string, s stepState, detail string) {
	r.pending = append(r.pending, pendingStep{label: label, state: s, detail: detail})
}

// flush prints the section's steps: everything that is news, and one line
// standing in for whatever was already in place. Verbose prints all of it.
func (r *report) flush(ctx context.Context) {
	steps := r.pending
	r.pending = nil

	current := 0
	for _, s := range steps {
		if s.state == stepCurrent && !r.verbose {
			current++
			continue
		}
		r.write(ctx, s.label, s.state, s.detail)
	}
	if current > 0 {
		r.write(ctx, "", stepCurrent, fmt.Sprintf("%d already current", current))
	}
}

// write renders one line, pausing first so a step a person cares about stays
// on screen long enough to read.
func (r *report) write(ctx context.Context, label string, s stepState, detail string) {
	r.pause(ctx, label)
	// The stand-in line has no label to align against, and already reads as a
	// sentence, so it takes neither the column nor the leading word.
	if label == "" {
		fmt.Fprintf(r.out, "    %s %s\n", r.view.bullet(s), r.view.muted(detail))
		return
	}
	line := fmt.Sprintf("    %s %-*s %s", r.view.bullet(s), labelWidth, label, r.view.muted(r.view.word(s)+detail))
	fmt.Fprintln(r.out, strings.TrimRight(line, " "))
}

// pause spins on the line the step is about to take, then clears it. Cosmetic,
// so it gives up as soon as the command is interrupted.
func (r *report) pause(ctx context.Context, label string) {
	if !r.view.animate || label == "" {
		return
	}
	defer fmt.Fprint(r.out, "\r\x1b[2K")
	deadline := time.Now().Add(stepPause)
	for i := 0; time.Now().Before(deadline); i++ {
		fmt.Fprintf(r.out, "\r    %s %s", r.view.muted(spinner[i%len(spinner)]), label)
		select {
		case <-ctx.Done():
			return
		case <-time.After(framePace):
		}
	}
}

// initView renders styled text via lipgloss, bound to the destination
// writer. Color and pacing both need a terminal: a pipe, a test, or
// NO_COLOR falls back to plain strings at full speed.
type initView struct {
	color   bool
	animate bool

	headingStyle lipgloss.Style
	nameStyle    lipgloss.Style
	successStyle lipgloss.Style
	warningStyle lipgloss.Style
	mutedStyle   lipgloss.Style
}

func newInitView(out io.Writer) initView {
	color := isTerminal(out) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"

	r := lipgloss.NewRenderer(out)
	if !color {
		r.SetColorProfile(termenv.Ascii)
	}
	return initView{
		color:        color,
		animate:      color,
		headingStyle: r.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		nameStyle:    r.NewStyle().Bold(true),
		successStyle: r.NewStyle().Foreground(lipgloss.Color("2")),
		warningStyle: r.NewStyle().Foreground(lipgloss.Color("3")),
		mutedStyle:   r.NewStyle().Faint(true),
	}
}

func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (v initView) heading(text string) string { return v.headingStyle.Render(text) }
func (v initView) name(text string) string    { return v.nameStyle.Render(text) }
func (v initView) success(text string) string { return v.successStyle.Render(text) }
func (v initView) warning(text string) string { return v.warningStyle.Render(text) }
func (v initView) muted(text string) string   { return v.mutedStyle.Render(text) }

func (v initView) bullet(s stepState) string {
	switch s {
	case stepCurrent:
		return v.muted("·")
	case stepPlanned:
		return v.muted("→")
	case stepWarn:
		return v.warning("!")
	default:
		return v.success("✓")
	}
}

// word carries what the bullet cannot; a completed step reads as its detail alone.
func (v initView) word(s stepState) string {
	switch s {
	case stepCurrent:
		return "already current, "
	case stepPlanned:
		return "pending, "
	default:
		return ""
	}
}
