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
