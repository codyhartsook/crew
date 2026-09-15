package delegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/role"
)

func testerDef() role.Definition {
	return role.Definition{
		Name: "tester", Harness: role.HarnessCodex, Memory: role.MemoryRole,
		Instructions: "Run the suite.",
		Render:       role.Render{Model: "gpt-5.3-codex", Effort: "medium"},
	}
}

func TestClaudeArgsIncludesPersonaAndSchema(t *testing.T) {
	args := claudeArgs(testerDef(), "run it", []byte(`{"type":"object"}`))
	got := strings.Join(args, " ")
	for _, want := range []string{`-p run it`, `--append-system-prompt Run the suite.`,
		`--model gpt-5.3-codex`, `--effort medium`, `--json-schema {"type":"object"}`,
		`--permission-mode acceptEdits`, `--permission-prompts none`, `--output-format json`} {
		if !strings.Contains(got, want) {
			t.Errorf("claudeArgs() = %q, want it to contain %q", got, want)
		}
	}
}

func TestClaudeArgsOmitsSchemaWhenUnset(t *testing.T) {
	args := claudeArgs(testerDef(), "run it", nil)
	if strings.Contains(strings.Join(args, " "), "--json-schema") {
		t.Errorf("claudeArgs() = %v, want no --json-schema", args)
	}
}

func TestCodexArgsComposesPersonaIntoThePrompt(t *testing.T) {
	args := codexArgs(testerDef(), "run it", "", "/tmp/result.json")
	if args[0] != "exec" || args[1] != "Run the suite.\n\nrun it" {
		t.Errorf("codexArgs()[:2] = %v, want [exec, \"Run the suite.\\n\\nrun it\"]", args[:2])
	}
	got := strings.Join(args, " ")
	for _, want := range []string{"-m gpt-5.3-codex", "-s workspace-write",
		"-c model_reasoning_effort=medium", "-o /tmp/result.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("codexArgs() = %q, want it to contain %q", got, want)
		}
	}
}

func TestCodexArgsDefaultsSandbox(t *testing.T) {
	def := testerDef()
	def.Render.Sandbox = ""
	args := codexArgs(def, "run it", "", "/tmp/result.json")
	if !strings.Contains(strings.Join(args, " "), "-s workspace-write") {
		t.Errorf("codexArgs() = %v, want the default sandbox", args)
	}
}

func TestCodexArgsUsesTheDefinitionsSandbox(t *testing.T) {
	def := testerDef()
	def.Render.Sandbox = "read-only"
	args := codexArgs(def, "run it", "", "/tmp/result.json")
	if !strings.Contains(strings.Join(args, " "), "-s read-only") {
		t.Errorf("codexArgs() = %v, want read-only", args)
	}
}

func TestCodexArgsPassesTheSchemaPath(t *testing.T) {
	args := codexArgs(testerDef(), "run it", "/tmp/schema.json", "/tmp/result.json")
	if !strings.Contains(strings.Join(args, " "), "--output-schema /tmp/schema.json") {
		t.Errorf("codexArgs() = %v, want the schema path", args)
	}
}

func TestLoadSchemaResolvesRelativeToTheDefinitionFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(dir, "schemas", "result.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	def := testerDef()
	def.Path = filepath.Join(dir, "tester.toml")
	def.Output.Schema = "schemas/result.json"

	got, err := loadSchema(def)
	if err != nil {
		t.Fatalf("loadSchema: %v", err)
	}
	if string(got) != `{"type":"object"}` {
		t.Errorf("loadSchema() = %s, want the schema file's content", got)
	}
}

func TestLoadSchemaIsNilWhenUnset(t *testing.T) {
	got, err := loadSchema(testerDef())
	if err != nil || got != nil {
		t.Errorf("loadSchema() = %v, %v, want nil, nil", got, err)
	}
}

func TestLoadSchemaRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(schemaPath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	def := testerDef()
	def.Path = filepath.Join(dir, "tester.toml")
	def.Output.Schema = "bad.json"

	if _, err := loadSchema(def); err == nil {
		t.Fatal("want an error for invalid JSON")
	}
}

func TestPickHarnessUsesTheDefinitionWhenConcrete(t *testing.T) {
	h, err := PickHarness(testerDef(), "")
	if err != nil || string(h) != "codex" {
		t.Errorf("PickHarness() = %q, %v, want codex, nil", h, err)
	}
}

func TestPickHarnessRequiresAFlagWhenAny(t *testing.T) {
	def := testerDef()
	def.Harness = role.HarnessAny
	if _, err := PickHarness(def, ""); err == nil {
		t.Fatal("want an error when the role allows either harness and none is chosen")
	}
	h, err := PickHarness(def, "claude")
	if err != nil || string(h) != "claude" {
		t.Errorf("PickHarness(claude) = %q, %v, want claude, nil", h, err)
	}
}

func TestPickHarnessRejectsAMismatchedFlag(t *testing.T) {
	if _, err := PickHarness(testerDef(), "claude"); err == nil {
		t.Fatal("want an error: role is codex-only")
	}
}

func TestParseClaudeOutputPrefersStructuredOutput(t *testing.T) {
	raw := []byte(`{"type":"result","is_error":false,"result":"{\"ok\":true}","structured_output":{"ok":true}}`)
	got, err := parseClaudeOutput(raw)
	if err != nil {
		t.Fatalf("parseClaudeOutput: %v", err)
	}
	if got != `{"ok":true}` {
		t.Errorf("parseClaudeOutput() = %q, want the structured output", got)
	}
}

func TestParseClaudeOutputFallsBackToResult(t *testing.T) {
	raw := []byte(`{"type":"result","is_error":false,"result":"plain text"}`)
	got, err := parseClaudeOutput(raw)
	if err != nil || got != "plain text" {
		t.Errorf("parseClaudeOutput() = %q, %v, want plain text, nil", got, err)
	}
}

func TestParseClaudeOutputReportsAnError(t *testing.T) {
	raw := []byte(`{"type":"result","is_error":true,"result":"boom"}`)
	if _, err := parseClaudeOutput(raw); err == nil {
		t.Fatal("want an error when is_error is true")
	}
}

// --bare skips Claude's hooks entirely and --ignore-user-config skips the
// file holding Codex's hook trust; either would silently break room join.
func TestSpawnArgsNeverSkipHooks(t *testing.T) {
	claude := strings.Join(claudeArgs(testerDef(), "task", nil), " ")
	if strings.Contains(claude, "--bare") {
		t.Error("claudeArgs() must never include --bare")
	}
	codex := strings.Join(codexArgs(testerDef(), "task", "", "/tmp/result.json"), " ")
	if strings.Contains(codex, "--ignore-user-config") {
		t.Error("codexArgs() must never include --ignore-user-config")
	}
}
