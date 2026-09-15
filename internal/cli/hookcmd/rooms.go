package hookcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/prune"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// hookOutput is the directive envelope both harnesses accept.
type hookOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// writeContext emits injected context, or nothing at all when there is none.
func writeContext(w io.Writer, event hook.Event, context string) error {
	if context == "" {
		return nil
	}
	out := hookOutput{HookSpecificOutput: hookSpecificOutput{
		HookEventName:     string(event),
		AdditionalContext: context,
	}}
	return json.NewEncoder(w).Encode(out)
}

// roomsFor gathers the context a hook should hand to its agent. Failures are
// returned but never fatal: a session that cannot read its room still works.
func roomsFor(ctx context.Context, st store.Store, event hook.Event, sess *session.Session, sessionKey string) (string, error) {
	rs, ok := st.(store.RoomStore)
	if !ok {
		return "", nil
	}

	switch event {
	case hook.EventStart:
		// Reap sessions whose process is gone before reporting who is here.
		prune.Quietly(ctx, st)
		briefing, err := briefingFor(ctx, st, rs, sess)
		if err != nil || sess == nil {
			return briefing, err
		}
		roster, err := rosterFor(ctx, st, sess, room.For(sess.Place))
		return joinContext(briefing, roster), err
	case hook.EventPrompt:
		// A turn is the only proof a session is still working, so it is what
		// keeps last-seen meaning last active rather than started.
		if sessionKey != "" {
			_ = st.Touch(ctx, sessionKey, time.Now().UTC())
		}
		roomNotice, err := noticeFor(ctx, rs, sessionKey)
		if err != nil {
			return roomNotice, err
		}
		delegationNotice, err := delegationNoticeFor(ctx, st, sessionKey)
		return joinContext(roomNotice, delegationNotice), err
	default:
		return "", nil
	}
}

func briefingFor(ctx context.Context, st store.Store, rs store.RoomStore, sess *session.Session) (string, error) {
	if sess == nil {
		return "", nil
	}
	here := room.For(sess.Place)
	if len(here) == 0 {
		return "", nil
	}
	if cmdutil.AutoJoinEnabled() {
		if err := roomctx.Join(ctx, rs, sess.Key(), here); err != nil {
			return "", err
		}
	}

	keys := room.Keys(here)
	entries, err := rs.Entries(ctx, room.Filter{Rooms: keys, Limit: roomctx.BriefingLimit * len(here) * 3})
	if err != nil {
		return "", err
	}
	rc := &roomctx.Context{Store: st, Rooms: rs, Here: here}
	others, err := rc.Others(ctx, sess.Key())
	if err != nil {
		return "", err
	}

	// Everything already in the room counts as delivered: the briefing shows it.
	if len(entries) > 0 {
		if err := rs.Ack(ctx, sess.Key(), entries[len(entries)-1].ID); err != nil {
			return "", err
		}
	}
	authors, err := rc.Authors(ctx)
	if err != nil {
		return "", err
	}
	return room.Briefing(here, entries, others, authors), nil
}

// noticeFor injects a nudge when something is waiting, nothing otherwise. It
// deliberately does not advance the cursor, so the notice persists until the
// agent actually reads rather than being mentioned once and missed.
func noticeFor(ctx context.Context, rs store.RoomStore, sessionKey string) (string, error) {
	if sessionKey == "" {
		return "", nil
	}
	// A session in no room has nothing to hear; skip the unread query entirely.
	rooms, err := rs.Rooms(ctx, sessionKey)
	if err != nil || len(rooms) == 0 {
		return "", err
	}
	entries, err := rs.Unread(ctx, sessionKey)
	if err != nil || len(entries) == 0 {
		return "", err
	}
	return room.Notice(entries), nil
}

// delegationNoticeFor surfaces the requester's finished delegations.
func delegationNoticeFor(ctx context.Context, st store.Store, sessionKey string) (string, error) {
	ds, ok := st.(store.DelegationStore)
	if !ok || sessionKey == "" {
		return "", nil
	}
	done, err := ds.ListDelegations(ctx, delegation.Filter{Requester: sessionKey, Status: delegation.StatusDone, Unnotified: true})
	if err != nil {
		return "", err
	}
	failed, err := ds.ListDelegations(ctx, delegation.Filter{Requester: sessionKey, Status: delegation.StatusFailed, Unnotified: true})
	if err != nil {
		return "", err
	}
	finished := append(done, failed...)
	if len(finished) == 0 {
		return "", nil
	}

	// Only what MarkNotified confirmed; a failure stays Unnotified for retry.
	var notified []*delegation.Delegation
	var markErr error
	for _, d := range finished {
		if err := ds.MarkNotified(ctx, d.ID); err != nil {
			markErr = errors.Join(markErr, err)
			continue
		}
		notified = append(notified, d)
	}
	return delegation.Notice(notified), markErr
}

func sessionKeyFrom(harness session.Harness, p hook.Payload) string {
	if p.SessionID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", harness, p.SessionID)
}
