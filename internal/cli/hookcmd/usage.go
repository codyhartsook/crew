package hookcmd

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// recordUsage stores session spend so work can route to the agent with the most
// headroom. Best effort: a failed read must never disturb a session.
func recordUsage(ctx context.Context, st store.Store, h session.Harness, p hook.Payload, sessionKey string) {
	if sessionKey == "" {
		return
	}
	// A turn is when spend changes; a start picks up a resumed session.
	switch p.Event() {
	case hook.EventStart, hook.EventPrompt:
	default:
		return
	}
	spec, ok := harness.For(h)
	if !ok || spec.Usage == nil {
		return
	}
	s, changed, err := spec.Usage.Open(p.SessionID, p.TranscriptPath).Refresh(ctx)
	if err != nil || !changed || !s.Known() {
		return
	}
	_ = st.SetUsage(ctx, sessionKey, &s)
}
