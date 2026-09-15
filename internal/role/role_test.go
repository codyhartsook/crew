package role

import (
	"strings"
	"testing"
)

func valid() Definition {
	return Definition{
		Name:         "tester",
		Description:  "Runs the full test suite",
		Harness:      HarnessCodex,
		Memory:       MemoryRole,
		Instructions: "Run the suite.",
		Path:         "tester.toml",
	}
}

func TestValidateAcceptsAWellFormedDefinition(t *testing.T) {
	d := valid()
	if err := d.Validate(); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}
}

func TestValidateRequiresName(t *testing.T) {
	d := valid()
	d.Name = ""
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for a missing name")
	}
}

func TestValidateRejectsAnUppercaseName(t *testing.T) {
	d := valid()
	d.Name = "Tester"
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for an uppercase name")
	}
}

func TestValidateRejectsAnUnknownHarness(t *testing.T) {
	d := valid()
	d.Harness = "gpt"
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for an unknown harness")
	}
}

func TestValidateRejectsAnUnknownMemory(t *testing.T) {
	d := valid()
	d.Memory = "forever"
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for an unknown memory scope")
	}
}

func TestValidateRequiresInstructions(t *testing.T) {
	d := valid()
	d.Instructions = ""
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for missing instructions")
	}
}

func TestValidateRejectsAnUnknownTriggerMode(t *testing.T) {
	d := valid()
	d.Triggers.Mode = "always"
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for an unknown trigger mode")
	}
}

func TestValidateRejectsAnUnparsablePromptPattern(t *testing.T) {
	d := valid()
	d.Triggers.Prompt = []string{"run tests("}
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for an invalid regexp")
	}
}

func TestValidateRejectsAToolTriggerMissingAPattern(t *testing.T) {
	d := valid()
	d.Triggers.Tool = []ToolTrigger{{Tool: "Bash"}}
	if err := d.Validate(); err == nil {
		t.Fatal("want an error for a tool trigger with no pattern")
	}
}

func TestValidateErrorNamesTheFile(t *testing.T) {
	d := valid()
	d.Name = ""
	d.Path = "/repo/.crew/agents/broken.toml"
	err := d.Validate()
	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); !strings.Contains(got, d.Path) {
		t.Errorf("error %q does not name the file %q", got, d.Path)
	}
}

func TestNormalizeDefaultsTriggerModeToSuggest(t *testing.T) {
	d := valid()
	d.normalize()
	if d.Triggers.Mode != TriggerSuggest {
		t.Errorf("Mode = %q, want %q", d.Triggers.Mode, TriggerSuggest)
	}
}

// A hot path matches every tool call against every active role's triggers;
// Validate compiling the pattern once, here, is what keeps that cheap.
func TestValidateCompilesToolTriggerPatterns(t *testing.T) {
	d := valid()
	d.Triggers.Tool = []ToolTrigger{{Tool: "Bash", Pattern: "^go test$"}}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	re := d.Triggers.Tool[0].Compiled()
	if re == nil {
		t.Fatal("Compiled() = nil, want the pattern compiled")
	}
	if !re.MatchString("go test") || re.MatchString("go test ./...") {
		t.Errorf("Compiled() did not compile %q", d.Triggers.Tool[0].Pattern)
	}
}

// Compiled must hand back the one instance Validate built, not recompile a
// fresh one on every call - that recompilation is exactly what made matching
// every tool call against every active role's triggers expensive.
func TestCompiledReturnsTheSameCachedInstance(t *testing.T) {
	d := valid()
	d.Triggers.Tool = []ToolTrigger{{Tool: "Bash", Pattern: "^go test$"}}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	first := d.Triggers.Tool[0].Compiled()
	second := d.Triggers.Tool[0].Compiled()
	if first != second {
		t.Error("Compiled() returned different instances across calls, want the same cached one")
	}
}
