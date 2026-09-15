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

func TestInactiveRolesOffersOnlyWhatIsNotActive(t *testing.T) {
	got := inactiveRoles(candidateDefs(), map[string]bool{"tester": true})
	if len(got) != 1 || got[0].Name != "reviewer" {
		t.Errorf("inactiveRoles() = %v, want only reviewer", got)
	}
}

func TestInactiveRolesReturnsNoneWhenEverythingIsActive(t *testing.T) {
	active := map[string]bool{"tester": true, "reviewer": true}
	if got := inactiveRoles(candidateDefs(), active); got != nil {
		t.Errorf("inactiveRoles() = %v, want none", got)
	}
}

func TestInactiveRolesWithNothingActiveOffersEverything(t *testing.T) {
	got := inactiveRoles(candidateDefs(), nil)
	if len(got) != 2 {
		t.Errorf("inactiveRoles() = %v, want both roles", got)
	}
}

// Nothing left to prompt for once every discovered role is already active:
// the interactive form never even starts, so nothing is printed.
func TestPromptActivationSkipsWhenEverythingIsAlreadyActive(t *testing.T) {
	var out strings.Builder
	active := map[string]bool{"tester": true, "reviewer": true}
	got := promptActivation(strings.NewReader(""), &out, candidateDefs(), active)
	if got != nil {
		t.Errorf("promptActivation() = %v, want none", got)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing printed", out.String())
	}
}
