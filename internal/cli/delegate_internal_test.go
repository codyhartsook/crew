package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func writeDelegateRole(t *testing.T, repo string) {
	t.Helper()
	writeRoleFile(t, filepath.Join(repo, ".crew", "agents"), "tester.toml", `
name = "tester"
description = "Runs the full test suite"
harness = "codex"
memory = "none"
instructions = "Run the suite."
`)
}

func TestDelegateRejectsAnUndefinedRole(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("HOME", home)
	t.Setenv("CREW_DB", filepath.Join(t.TempDir(), "sessions.db"))
	t.Chdir(repo)

	root := New()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"delegate", "ghost", "do something"})
	if err := root.Execute(); err == nil {
		t.Fatal("want an error delegating to an undefined role")
	}
}

func TestDelegateQueuesByDefault(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("HOME", home)
	t.Setenv("CREW_DB", dbPath)
	t.Setenv("CODEX_THREAD_ID", "launcher")
	t.Chdir(repo)
	writeDelegateRole(t, repo)
	seedActiveSession(t, dbPath, repo, "launcher")

	root := New()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"delegate", "tester", "run the suite"})
	if err := root.Execute(); err != nil {
		t.Fatalf("delegate: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "queued") {
		t.Errorf("output = %q, want a queued confirmation", out.String())
	}

	st, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := strings.Fields(out.String())[1]

	root2 := New()
	var resultOut strings.Builder
	root2.SetOut(&resultOut)
	root2.SetErr(&resultOut)
	root2.SetArgs([]string{"delegate", "result", id})
	if err := root2.Execute(); err != nil {
		t.Fatalf("delegate result: %v", err)
	}
	if !strings.Contains(resultOut.String(), "queued, not started yet") {
		t.Errorf("result output = %q, want it still pending (no broker running)", resultOut.String())
	}
}

func TestDelegateResultRejectsAnUnknownID(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("HOME", home)
	t.Setenv("CREW_DB", filepath.Join(t.TempDir(), "sessions.db"))
	t.Chdir(repo)

	root := New()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"delegate", "result", "no-such-id"})
	if err := root.Execute(); err == nil {
		t.Fatal("want an error for an unknown delegation id")
	}
}
