package routing_test

import (
	"testing"

	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/routing"
)

func enforcingDef(name string) role.Definition {
	return role.Definition{
		Name: name,
		Triggers: role.Triggers{
			Mode: role.TriggerEnforce,
			Tool: []role.ToolTrigger{{Tool: "Bash", Pattern: `^go test \./\.\.\.$`}},
		},
	}
}

func TestMatchToolFindsAnEnforcedExactMatch(t *testing.T) {
	def, ok := routing.MatchTool([]role.Definition{enforcingDef("tester")}, "Bash", "go test ./...")
	if !ok || def.Name != "tester" {
		t.Errorf("MatchTool() = %+v, %v, want tester, true", def, ok)
	}
}

func TestMatchToolIgnoresSuggestModeRoles(t *testing.T) {
	def := enforcingDef("tester")
	def.Triggers.Mode = role.TriggerSuggest
	if _, ok := routing.MatchTool([]role.Definition{def}, "Bash", "go test ./..."); ok {
		t.Error("MatchTool() matched a suggest-mode role, want none")
	}
}

func TestMatchToolIsACarveOutForTargetedWork(t *testing.T) {
	if _, ok := routing.MatchTool([]role.Definition{enforcingDef("tester")}, "Bash", "go test ./internal/foo/..."); ok {
		t.Error("MatchTool() matched a targeted command, want the exact-match pattern to pass it through")
	}
}

func TestMatchToolIgnoresADifferentTool(t *testing.T) {
	if _, ok := routing.MatchTool([]role.Definition{enforcingDef("tester")}, "Read", "go test ./..."); ok {
		t.Error("MatchTool() matched the wrong tool, want none")
	}
}
