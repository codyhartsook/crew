package role

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testerTOML = `
name = "tester"
description = "Runs the full test suite and reports failures"
harness = "codex"
memory = "role"
instructions = "Run the suite."

[render]
model = "gpt-5.3-codex"
`

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOfAMissingDirectoryIsNotAnError(t *testing.T) {
	defs, err := Load(filepath.Join(t.TempDir(), "nope"), ScopeRepo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if len(defs) != 0 {
		t.Errorf("Load() = %v, want none", defs)
	}
}

func TestLoadReadsAWellFormedDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tester.toml", testerTOML)

	defs, err := Load(dir, ScopeRepo)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "tester" {
		t.Fatalf("Load() = %+v, want one definition named tester", defs)
	}
	if defs[0].Scope != ScopeRepo {
		t.Errorf("Scope = %q, want %q", defs[0].Scope, ScopeRepo)
	}
	if defs[0].Harness != HarnessCodex {
		t.Errorf("Harness = %q, want %q", defs[0].Harness, HarnessCodex)
	}
}

func TestLoadFailsOnAnInvalidDefinitionAndNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "broken.toml", `description = "no name"`+"\n")

	_, err := Load(dir, ScopeRepo)
	if err == nil {
		t.Fatal("want an error for a definition missing its name")
	}
	if !strings.Contains(err.Error(), filepath.Join(dir, "broken.toml")) {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestLoadFailsOnAnUnknownField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "typo.toml", testerTOML+"\nharnes = \"codex\"\n")

	_, err := Load(dir, ScopeRepo)
	if err == nil {
		t.Fatal("want an error for an unknown field")
	}
	if !strings.Contains(err.Error(), "harnes") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

func TestLoadIgnoresNonTOMLFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tester.toml", testerTOML)
	writeFile(t, dir, "README.md", "not a role")

	defs, err := Load(dir, ScopeRepo)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("Load() = %+v, want only the .toml file", defs)
	}
}

func TestDiscoverRepoWinsOnNameCollision(t *testing.T) {
	repoDir, globalDir := t.TempDir(), t.TempDir()
	writeFile(t, repoDir, "tester.toml", strings.Replace(testerTOML, "gpt-5.3-codex", "repo-model", 1))
	writeFile(t, globalDir, "tester.toml", strings.Replace(testerTOML, "gpt-5.3-codex", "global-model", 1))

	reg, err := Discover(repoDir, globalDir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	got, ok := reg.Get("tester")
	if !ok {
		t.Fatal("tester not found")
	}
	if got.Scope != ScopeRepo || got.Render.Model != "repo-model" {
		t.Errorf("got %+v, want the repo definition to win", got)
	}
	if shadowed, ok := reg.Shadowed("tester"); !ok || shadowed.Scope != ScopeGlobal {
		t.Errorf("Shadowed(tester) = %+v, %v, want the global definition", shadowed, ok)
	}
}

func TestDiscoverMergesDistinctNamesFromBothScopes(t *testing.T) {
	repoDir, globalDir := t.TempDir(), t.TempDir()
	writeFile(t, repoDir, "tester.toml", testerTOML)
	writeFile(t, globalDir, "reviewer.toml", strings.Replace(testerTOML, "\"tester\"", "\"reviewer\"", 1))

	reg, err := Discover(repoDir, globalDir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("All() = %+v, want both roles", all)
	}
	if _, ok := reg.Shadowed("tester"); ok {
		t.Error("tester should not be shadowed")
	}
}

func TestDiscoverWithNoRepoDirOnlyReadsGlobal(t *testing.T) {
	globalDir := t.TempDir()
	writeFile(t, globalDir, "tester.toml", testerTOML)

	reg, err := Discover("", globalDir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(reg.All()) != 1 {
		t.Fatalf("All() = %+v, want the one global role", reg.All())
	}
}

// DiscoverFor is the glue every caller that already knows the repo root
// needs: home for global, RepoDir/GlobalDir joined on for each scope.
func TestDiscoverForReadsBothScopesFromAKnownRepoRoot(t *testing.T) {
	home, repoRoot := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, GlobalDir(home), "reviewer.toml", strings.Replace(testerTOML, `"tester"`, `"reviewer"`, 1))
	writeFile(t, RepoDir(repoRoot), "tester.toml", testerTOML)

	reg, err := DiscoverFor(repoRoot)
	if err != nil {
		t.Fatalf("DiscoverFor() error = %v", err)
	}
	// Named rather than counted: DiscoverFor also merges the embedded roles,
	// so a count breaks every time one is added.
	for name, want := range map[string]Scope{"reviewer": ScopeGlobal, "tester": ScopeRepo} {
		got, ok := reg.Get(name)
		if !ok {
			t.Fatalf("All() = %+v, want it to include %s", reg.All(), name)
		}
		if got.Scope != want {
			t.Errorf("%s scope = %v, want %v", name, got.Scope, want)
		}
	}
}

func TestDiscoverForWithNoRepoRootOnlyReadsGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, GlobalDir(home), "reviewer.toml", strings.Replace(testerTOML, `"tester"`, `"reviewer"`, 1))

	reg, err := DiscoverFor("")
	if err != nil {
		t.Fatalf("DiscoverFor() error = %v", err)
	}
	got, ok := reg.Get("reviewer")
	if !ok || got.Scope != ScopeGlobal {
		t.Errorf("Get(reviewer) = %+v, %v, want the global role", got, ok)
	}
}

func TestDiscoverForFillsInEmbeddedDefaultsNotDefinedElsewhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	reg, err := DiscoverFor("")
	if err != nil {
		t.Fatalf("DiscoverFor() error = %v", err)
	}
	got, ok := reg.Get("tester")
	if !ok || got.Scope != ScopeEmbedded {
		t.Errorf("Get(tester) = %+v, %v, want the embedded default", got, ok)
	}
}

func TestDiscoverForLetsARepoRoleOverrideTheEmbeddedDefault(t *testing.T) {
	home, repoRoot := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, RepoDir(repoRoot), "tester.toml", strings.Replace(testerTOML, "gpt-5.3-codex", "repo-model", 1))

	reg, err := DiscoverFor(repoRoot)
	if err != nil {
		t.Fatalf("DiscoverFor() error = %v", err)
	}
	got, ok := reg.Get("tester")
	if !ok || got.Scope != ScopeRepo || got.Render.Model != "repo-model" {
		t.Errorf("Get(tester) = %+v, %v, want the repo definition to win over the embedded default", got, ok)
	}
}

// DiscoverFromDir adds detection on top of DiscoverFor: given a plain
// directory inside a repo, it must find that repo's own role definitions.
func TestDiscoverFromDirDetectsTheRepoRoot(t *testing.T) {
	home, repoRoot := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	if out, err := exec.Command("git", "init", "-q", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	writeFile(t, RepoDir(repoRoot), "tester.toml", testerTOML)

	reg, err := DiscoverFromDir(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("DiscoverFromDir() error = %v", err)
	}
	if _, ok := reg.Get("tester"); !ok {
		t.Errorf("All() = %+v, want the repo's tester role", reg.All())
	}
}
