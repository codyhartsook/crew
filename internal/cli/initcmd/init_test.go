package initcmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDashboardOpensUnlessHeadless(t *testing.T) {
	original := openDashboard
	t.Cleanup(func() { openDashboard = original })
	opened := ""
	openDashboard = func(_ context.Context, url string, _ bool) error {
		opened = url
		return nil
	}

	if err := maybeOpenDashboard(context.Background(), "http://localhost", false); err != nil {
		t.Fatal(err)
	}
	if opened != "http://localhost" {
		t.Fatalf("opened %q, want dashboard URL", opened)
	}
	opened = ""
	if err := maybeOpenDashboard(context.Background(), "http://localhost", true); err != nil {
		t.Fatal(err)
	}
	if opened != "" {
		t.Fatalf("headless opened %q", opened)
	}
}

// A "go run" binary lives in the build cache and is deleted on exit; recording
// its path would leave hooks that fail silently forever.
func TestTransient(t *testing.T) {
	for _, path := range []string{
		"/var/folders/xx/T/go-build123/b001/exe/crew",
		filepath.Join(os.TempDir(), "mp"),
	} {
		if !transient(path) {
			t.Errorf("transient(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"/usr/local/bin/crew", "/Users/x/.local/bin/crew"} {
		if transient(path) {
			t.Errorf("transient(%q) = true, want false", path)
		}
	}
}

func TestInitViewStaysPlainOutsideATerminal(t *testing.T) {
	var out strings.Builder
	view := newInitView(&out)
	if got := view.heading("Setup"); got != "Setup" {
		t.Errorf("heading = %q, want plain text", got)
	}
}

// Captured output must skip both the color and the spinner.
func TestReportStaysPlainAndInstantOffATerminal(t *testing.T) {
	var out strings.Builder
	rep := newReport(&out)
	if rep.view.animate {
		t.Error("a captured writer is being animated")
	}

	start := time.Now()
	rep.section("Claude Code", "~/.claude/settings.json")
	rep.step(context.Background(), "SessionStart hook", stepDone, "10s timeout")
	rep.step(context.Background(), "SessionEnd hook", stepCurrent, "5s timeout")
	rep.step(context.Background(), "room skill", stepPlanned, "~/.claude/skills/crew-rooms")
	if elapsed := time.Since(start); elapsed > stepPause {
		t.Errorf("rendering took %s, want no pacing off a terminal", elapsed)
	}

	want := "\n  Claude Code  ~/.claude/settings.json\n" +
		"    ✓ SessionStart hook      10s timeout\n" +
		"    · SessionEnd hook        already current, 5s timeout\n" +
		"    → room skill             pending, ~/.claude/skills/crew-rooms\n"
	if got := out.String(); got != want {
		t.Errorf("report =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("captured output carries escape codes")
	}
}

// --dry-run must never claim a change it has not made.
func TestStateSeparatesPlannedFromDone(t *testing.T) {
	for _, tc := range []struct {
		dryRun, changed bool
		want            stepState
	}{
		{dryRun: false, changed: true, want: stepDone},
		{dryRun: true, changed: true, want: stepPlanned},
		{dryRun: false, changed: false, want: stepCurrent},
		{dryRun: true, changed: false, want: stepCurrent},
	} {
		if got := state(tc.dryRun, tc.changed); got != tc.want {
			t.Errorf("state(dryRun=%v, changed=%v) = %v, want %v", tc.dryRun, tc.changed, got, tc.want)
		}
	}
}

func TestTildeShortensPathsUnderHome(t *testing.T) {
	home := filepath.Join("/Users", "someone")
	if got := tilde(home, filepath.Join(home, ".claude", "settings.json")); got != filepath.Join("~", ".claude", "settings.json") {
		t.Errorf("tilde = %q, want the home prefix replaced", got)
	}
	if got := tilde(home, "/etc/crew.json"); got != "/etc/crew.json" {
		t.Errorf("tilde = %q, want a path outside home left alone", got)
	}
	// A sibling that shares a prefix with home is not inside it.
	if got := tilde(home, "/Users/someone-else/x"); got != "/Users/someone-else/x" {
		t.Errorf("tilde = %q, want a sibling of home left alone", got)
	}
}
