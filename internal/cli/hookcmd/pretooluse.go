package hookcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/routing"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// preToolUseOutput is the deny envelope, verified identical on Claude 2.1.270
// and Codex 0.154.0.
type preToolUseOutput struct {
	HookSpecificOutput preToolUseSpecificOutput `json:"hookSpecificOutput"`
}

type preToolUseSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// writeDecision emits a deny, or nothing at all to allow the call through.
func writeDecision(w io.Writer, deny bool, reason string) error {
	if !deny {
		return nil
	}
	out := preToolUseOutput{HookSpecificOutput: preToolUseSpecificOutput{
		HookEventName:            string(hook.EventPreToolUse),
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}}
	return json.NewEncoder(w).Encode(out)
}

// enforceFor is tier 2: deny an exact, enforced tool call and redirect it to
// the role that owns it. sessionKey is the caller's own sessionKeyFrom(h, p).
func enforceFor(ctx context.Context, st store.Store, sessionKey string, p hook.Payload) (deny bool, reason string, err error) {
	if p.ToolName == "" || sessionKey == "" {
		return false, "", nil
	}
	// A native subagent (Claude's Task tool) cannot delegate further and
	// carries no crew identity of its own; intercepting it would only confuse.
	if p.AgentID != "" {
		return false, "", nil
	}
	rs, ok := st.(store.RoleStore)
	if !ok {
		return false, "", nil
	}
	sess, err := st.Get(ctx, sessionKey)
	if err != nil {
		// Unknown session: fail open rather than block on a guess.
		return false, "", nil
	}

	names, err := activeRoleNames(ctx, rs, room.For(sess.Place))
	if err != nil || len(names) == 0 {
		return false, "", err
	}
	defs := defsFor(names, discoverRoles(sess))

	match, hit := routing.MatchTool(defs, p.ToolName, p.ToolTarget())
	if !hit {
		return false, "", nil
	}
	// The role's own delegated spawn must still be able to do its job.
	if os.Getenv(cmdutil.EnvRole) == match.Name {
		return false, "", nil
	}
	return true, fmt.Sprintf("crew: this is handled by role %q; run `crew delegate %s \"<task>\"` instead of running it inline.",
		match.Name, match.Name), nil
}
