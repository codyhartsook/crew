package initcmd

import (
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/role"
)

func candidateDefs() []role.Definition {
	return []role.Definition{
		{Name: "tester", Description: "Runs the suite", Harness: role.HarnessCodex},
		{Name: "reviewer", Description: "Reviews a diff", Harness: role.HarnessAny},
	}
}

func TestPromptActivationSkipsOnBlankAnswer(t *testing.T) {
	var out strings.Builder
	got := promptActivation(strings.NewReader("\n"), &out, initView{}, candidateDefs(), nil)
	if got != nil {
		t.Errorf("promptActivation() = %v, want none", got)
	}
}

func TestPromptActivationParsesACommaList(t *testing.T) {
	var out strings.Builder
	got := promptActivation(strings.NewReader("tester, reviewer\n"), &out, initView{}, candidateDefs(), nil)
	if len(got) != 2 || got[0] != "tester" || got[1] != "reviewer" {
		t.Errorf("promptActivation() = %v, want [tester reviewer]", got)
	}
}

func TestPromptActivationIgnoresUnknownNames(t *testing.T) {
	var out strings.Builder
	got := promptActivation(strings.NewReader("tester, ghost\n"), &out, initView{}, candidateDefs(), nil)
	if len(got) != 1 || got[0] != "tester" {
		t.Errorf("promptActivation() = %v, want only tester", got)
	}
}

func TestPromptActivationAllSelectsEveryCandidate(t *testing.T) {
	var out strings.Builder
	got := promptActivation(strings.NewReader("all\n"), &out, initView{}, candidateDefs(), nil)
	if len(got) != 2 {
		t.Errorf("promptActivation() = %v, want both roles", got)
	}
}

// Nothing left to prompt for once every discovered role is already active:
// asking again would just be noise.
func TestPromptActivationSkipsWhenEverythingIsAlreadyActive(t *testing.T) {
	var out strings.Builder
	active := map[string]bool{"tester": true, "reviewer": true}
	got := promptActivation(strings.NewReader("all\n"), &out, initView{}, candidateDefs(), active)
	if got != nil {
		t.Errorf("promptActivation() = %v, want none", got)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing printed", out.String())
	}
}

func TestPromptActivationOffersOnlyInactiveRoles(t *testing.T) {
	var out strings.Builder
	active := map[string]bool{"tester": true}
	got := promptActivation(strings.NewReader("all\n"), &out, initView{}, candidateDefs(), active)
	if len(got) != 1 || got[0] != "reviewer" {
		t.Errorf("promptActivation() = %v, want only reviewer", got)
	}
}
