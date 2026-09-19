package hookcmd

import (
	"context"
	"os"

	"github.com/codyhartsook/multiplayer/internal/channel"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// recordDelegationChild tells a delegation which session key its role runs as.
// Only the child knows: the spawn hands it CREW_DELEGATION.
func recordDelegationChild(ctx context.Context, st store.Store, event hook.Event, sessionKey string) error {
	if event != hook.EventStart || sessionKey == "" {
		return nil
	}
	id := os.Getenv(cmdutil.EnvDelegation)
	if id == "" {
		return nil
	}
	ds, ok := st.(store.DelegationStore)
	if !ok {
		return nil
	}
	return ds.SetDelegationChild(ctx, id, sessionKey)
}

// askNoticeFor surfaces the questions waiting on this session. It closes
// nothing, so the notice persists until the answer is written.
func askNoticeFor(ctx context.Context, st store.Store, sessionKey string) (string, error) {
	cs, ok := st.(store.ChannelStore)
	if !ok || sessionKey == "" {
		return "", nil
	}
	open, err := cs.Inbox(ctx, channel.Addr(sessionKey))
	if err != nil || len(open) == 0 {
		return "", err
	}
	return channel.Notice(open), nil
}
