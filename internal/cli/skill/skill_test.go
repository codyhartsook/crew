package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillKeepsAgentOperatingContext(t *testing.T) {
	for _, want := range []string{
		"crew ls",
		"always use `--to`",
		"Search before anything multi-step",
		"Write for an agent with no context",
		"crew role <name> \"<description>\"",
		"for the other agents deciding what to hand you",
		"check it before you send",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("skill is missing %q", want)
		}
	}
}

// Handing off a review is only worth it if the reviewer attacks the change and
// hands the verdict back, rather than quietly fixing the author's tree.
func TestSkillKeepsAdversarialReviewStance(t *testing.T) {
	for _, want := range []string{
		"## Adversarial review",
		"try to disprove the change rather than confirm it",
		"do not fix what you find",
		"review your own change",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("skill is missing %q", want)
		}
	}
}

// The skill is meant to be tuned once you see what agents write, so a re-init
// must not overwrite an edit.
func TestInstallPreservesLocalEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if got, err := Install(path, doc, false); err != nil || got != Written {
		t.Fatalf("first init = (%v, %v), want written", got, err)
	}
	if got, err := Install(path, doc, false); err != nil || got != Unchanged {
		t.Fatalf("re-init unchanged = (%v, %v), want unchanged", got, err)
	}

	// An older version this tool wrote is safe to replace.
	if err := os.WriteFile(path, []byte("an older generated skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, markerFile), []byte(digest([]byte("an older generated skill"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Install(path, doc, false); err != nil || got != Written {
		t.Fatalf("replacing our own older output = (%v, %v), want written", got, err)
	}

	// An edit is not.
	edited := append(append([]byte{}, doc...), []byte("\nMY TUNING\n")...)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Install(path, doc, false)
	if err != nil || got != Preserved {
		t.Fatalf("re-init over an edit = (%v, %v), want preserved", got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(after), "MY TUNING") {
		t.Error("the local edit was overwritten")
	}
	if _, err := os.Stat(path + ".new"); err != nil {
		t.Error("no .new written alongside the preserved skill")
	}
}

// A skill initialized before markers existed is still ours, and must not be
// mistaken for a local edit the first time it needs updating.
func TestInstallAdoptsAnUnmarkedCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, doc, 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := Install(path, doc, false); err != nil || got != Unchanged {
		t.Fatalf("init over an identical unmarked copy = (%v, %v), want unchanged", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, markerFile)); err != nil {
		t.Fatal("no marker recorded for a copy we recognised as ours")
	}
	// Now a genuine update replaces it rather than preserving it.
	if err := os.WriteFile(path, []byte("pretend this is an older generated version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, markerFile), []byte(digest([]byte("pretend this is an older generated version"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Install(path, doc, false); err != nil || got != Written {
		t.Fatalf("update = (%v, %v), want written", got, err)
	}
}

// The command was renamed, so a skill left at the old name would keep telling
// agents to run a command that is gone. One you edited is still yours.
func TestRemoveLegacy(t *testing.T) {
	skills := t.TempDir()
	old := filepath.Join(skills, legacyName)
	path := filepath.Join(old, "SKILL.md")
	if _, err := Install(path, doc, false); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveLegacy(skills, true)
	if err != nil || !removed {
		t.Fatalf("dry run = (%v, %v), want removed", removed, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("dry run deleted the skill")
	}

	if removed, err := RemoveLegacy(skills, false); err != nil || !removed {
		t.Fatalf("remove = (%v, %v), want removed", removed, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the legacy skill directory is still there")
	}

	// An edited copy is kept, and reports nothing removed.
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("my own skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := RemoveLegacy(skills, false); err != nil || removed {
		t.Fatalf("remove over an edit = (%v, %v), want kept", removed, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("an edited legacy skill was deleted")
	}
}

// Nothing to sweep is the common case and must not be an error.
func TestRemoveLegacyWithNothingInstalled(t *testing.T) {
	if removed, err := RemoveLegacy(t.TempDir(), false); err != nil || removed {
		t.Fatalf("remove = (%v, %v), want nothing", removed, err)
	}
}
