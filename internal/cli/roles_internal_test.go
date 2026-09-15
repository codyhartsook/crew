package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const reviewerTOML = `
name = "reviewer"
description = "Reviews a diff for correctness bugs"
harness = "any"
memory = "session"
instructions = "Review the diff."
`

// runRoles executes "crew roles" with home as $HOME, from cwd, and returns
// its output.
func runRoles(t *testing.T, home, cwd string, args ...string) string {
	t.Helper()
	t.Setenv("HOME", home)
	t.Chdir(cwd)

	var out strings.Builder
	root := New()
	root.SetContext(context.Background())
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"roles"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("roles %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func writeRoleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRolesFallsBackToTheEmbeddedDefaultWhenNoneAreDefined(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	out := runRoles(t, home, cwd)
	if !strings.Contains(out, "tester") || !strings.Contains(out, "embedded") {
		t.Errorf("output = %q, want the embedded tester default", out)
	}
}

func TestRolesListsAGlobalDefinition(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	writeRoleFile(t, filepath.Join(home, ".crew", "agents"), "reviewer.toml", reviewerTOML)

	out := runRoles(t, home, cwd)
	if !strings.Contains(out, "reviewer") || !strings.Contains(out, "global") {
		t.Errorf("output = %q, want the global reviewer role", out)
	}
}

// A repo definition of the same name as a global one wins, and the listing
// says so, since that is the only thing left to point at once the loser is
// no longer shown as its own row.
func TestRolesRepoDefinitionWinsAndNotesTheShadow(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	writeRoleFile(t, filepath.Join(home, ".crew", "agents"), "tester.toml",
		strings.Replace(reviewerTOML, `"reviewer"`, `"tester"`, 1))
	writeRoleFile(t, filepath.Join(repo, ".crew", "agents"), "tester.toml",
		strings.Replace(reviewerTOML, `"reviewer"`, `"tester"`, 1))

	out := runRoles(t, home, repo)
	if !strings.Contains(out, "tester") || !strings.Contains(out, "repo") {
		t.Errorf("output = %q, want the repo tester role", out)
	}
	if !strings.Contains(out, "also defined globally") {
		t.Errorf("output = %q, want a note about the shadowed global definition", out)
	}
}

func TestRolesJSONEmitsEveryDefinition(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	writeRoleFile(t, filepath.Join(home, ".crew", "agents"), "reviewer.toml", reviewerTOML)

	out := runRoles(t, home, cwd, "--json")
	if !strings.Contains(out, `"name": "reviewer"`) {
		t.Errorf("output = %q, want reviewer in JSON", out)
	}
}

func TestRolesActivateMarksItActive(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("CREW_DB", filepath.Join(t.TempDir(), "sessions.db"))
	writeRoleFile(t, filepath.Join(repo, ".crew", "agents"), "reviewer.toml", reviewerTOML)

	if out := runRoles(t, home, repo, "activate", "reviewer"); !strings.Contains(out, "active") {
		t.Fatalf("activate output = %q, want confirmation", out)
	}
	out := runRoles(t, home, repo)
	if !strings.Contains(out, "yes") {
		t.Errorf("output = %q, want reviewer marked active", out)
	}
}

func TestRolesActivateRejectsAnUndefinedRole(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("CREW_DB", filepath.Join(t.TempDir(), "sessions.db"))
	t.Setenv("HOME", home)
	t.Chdir(repo)

	root := New()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"roles", "activate", "ghost"})
	if err := root.Execute(); err == nil {
		t.Fatal("want an error activating an undefined role")
	}
}

func TestRolesDeactivateTurnsItOff(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("CREW_DB", filepath.Join(t.TempDir(), "sessions.db"))
	writeRoleFile(t, filepath.Join(repo, ".crew", "agents"), "reviewer.toml", reviewerTOML)

	runRoles(t, home, repo, "activate", "reviewer")
	runRoles(t, home, repo, "deactivate", "reviewer")
	out := runRoles(t, home, repo)
	if strings.Contains(out, "yes") {
		t.Errorf("output = %q, want reviewer no longer active", out)
	}
}
