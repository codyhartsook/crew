package hookcmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

const enforcingTesterTOML = `
name = "tester"
description = "Runs the full test suite and reports failures"
harness = "codex"
memory = "none"
instructions = "Run the suite."

[triggers]
mode = "enforce"
tool = [{ tool = "Bash", pattern = "^go test \\./\\.\\.\\.$" }]
`

func setupEnforcingRole(t *testing.T) (st *sqlitestore.Store, repo string) {
	t.Helper()
	ctx := context.Background()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	writeRoleFile(t, filepath.Join(repo, ".crew", "agents"), "tester.toml", enforcingTesterTOML)

	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.ActivateRole(ctx, &role.Activation{Room: repo, Role: "tester", ActivatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{
		ID: "caller", Harness: session.HarnessCodex, Status: session.StatusActive,
		Place:     session.Place{CWD: repo, Repo: &session.Repo{Name: "repo", Root: repo, MainRoot: repo}},
		StartedAt: time.Now(), LastSeen: time.Now(),
	}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
	return st, repo
}

func toolPayload(sessionID, toolName, command string) hook.Payload {
	return hook.Payload{
		SessionID: sessionID, HookEventName: "PreToolUse",
		ToolName: toolName, ToolInput: []byte(`{"command":"` + command + `"}`),
	}
}

func TestEnforceDeniesAnExactEnforcedMatch(t *testing.T) {
	st, _ := setupEnforcingRole(t)
	deny, reason, err := enforceFor(context.Background(), st, "codex:caller", toolPayload("caller", "Bash", "go test ./..."))
	if err != nil {
		t.Fatalf("enforceFor: %v", err)
	}
	if !deny || reason == "" {
		t.Errorf("enforceFor() = %v, %q, want a deny naming the role", deny, reason)
	}
}

func TestEnforceIsACarveOutForTargetedWork(t *testing.T) {
	st, _ := setupEnforcingRole(t)
	deny, _, err := enforceFor(context.Background(), st, "codex:caller", toolPayload("caller", "Bash", "go test ./internal/foo/..."))
	if err != nil {
		t.Fatalf("enforceFor: %v", err)
	}
	if deny {
		t.Error("enforceFor() denied a targeted command, want it to pass through")
	}
}

// The delegated role's own spawn must still be able to run the very command
// it was delegated to run, or tier 2 would trap it in a loop.
func TestEnforceExemptsTheDelegatedRoleItself(t *testing.T) {
	st, _ := setupEnforcingRole(t)
	t.Setenv("CREW_ROLE", "tester")
	deny, _, err := enforceFor(context.Background(), st, "codex:caller", toolPayload("caller", "Bash", "go test ./..."))
	if err != nil {
		t.Fatalf("enforceFor: %v", err)
	}
	if deny {
		t.Error("enforceFor() denied the delegated role's own spawn, want it exempt")
	}
}

// A native Claude subagent cannot delegate further and carries no crew
// identity of its own, so it is never intercepted.
func TestEnforceExemptsANativeSubagent(t *testing.T) {
	st, _ := setupEnforcingRole(t)
	p := toolPayload("caller", "Bash", "go test ./...")
	p.AgentID = "a43987ac49c316edd"
	deny, _, err := enforceFor(context.Background(), st, "codex:caller", p)
	if err != nil {
		t.Fatalf("enforceFor: %v", err)
	}
	if deny {
		t.Error("enforceFor() denied a native subagent's call, want it exempt")
	}
}

func TestEnforceAllowsAnUnmatchedTool(t *testing.T) {
	st, _ := setupEnforcingRole(t)
	deny, _, err := enforceFor(context.Background(), st, "codex:caller", toolPayload("caller", "Read", "go test ./..."))
	if err != nil {
		t.Fatalf("enforceFor: %v", err)
	}
	if deny {
		t.Error("enforceFor() denied an unmatched tool, want it allowed")
	}
}

// An unknown session must fail open: blocking on a guess is worse than not
// routing at all.
func TestEnforceFailsOpenForAnUnknownSession(t *testing.T) {
	st, _ := setupEnforcingRole(t)
	deny, _, err := enforceFor(context.Background(), st, "codex:ghost-session", toolPayload("ghost-session", "Bash", "go test ./..."))
	if err != nil {
		t.Fatalf("enforceFor: %v", err)
	}
	if deny {
		t.Error("enforceFor() denied for an unknown session, want it allowed")
	}
}

// An empty session key (an empty SessionID in the payload) must be treated
// the same way sessionKeyFrom treats it everywhere else: no lookup at all,
// not a lookup for a literal "codex:" key.
func TestEnforceAllowsWhenSessionKeyIsEmpty(t *testing.T) {
	st, _ := setupEnforcingRole(t)
	deny, _, err := enforceFor(context.Background(), st, "", toolPayload("", "Bash", "go test ./..."))
	if err != nil {
		t.Fatalf("enforceFor: %v", err)
	}
	if deny {
		t.Error("enforceFor() denied with an empty session key, want it allowed")
	}
}
