package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// autoJoinDefault governs whether a starting session joins its rooms without
// being asked. MULTIPLAYER_AUTO_JOIN overrides it.
const autoJoinDefault = true

// stateLimitPerRoom bounds how much state a briefing pulls per room.
const stateLimitPerRoom = 16

func autoJoinEnabled() bool {
	switch os.Getenv(envAutoJoin) {
	case "":
		return autoJoinDefault
	case "0", "false", "no":
		return false
	default:
		return true
	}
}

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
		pruneQuietly(ctx, st)
		return briefingFor(ctx, st, rs, sess)
	case hook.EventPrompt:
		// A turn is the only proof a session is still working, so it is what
		// keeps last-seen meaning last active rather than started.
		if sessionKey != "" {
			_ = st.Touch(ctx, sessionKey, time.Now().UTC())
		}
		return noticeFor(ctx, rs, sessionKey)
	default:
		return "", nil
	}
}

func briefingFor(ctx context.Context, st store.Store, rs store.RoomStore, sess *session.Session) (string, error) {
	if sess == nil {
		return "", nil
	}
	here := room.For(sess.Repo, sess.Treehouse, sess.CWD)
	if len(here) == 0 {
		return "", nil
	}
	if autoJoinEnabled() {
		if err := JoinRooms(ctx, rs, sess.Key(), here); err != nil {
			return "", err
		}
	}

	keys := room.Keys(here)
	entries, err := rs.Entries(ctx, room.Filter{Rooms: keys, Limit: briefingLimit * len(here) * 3})
	if err != nil {
		return "", err
	}
	reviews, err := rs.Reviews(ctx, room.ReviewFilter{Rooms: keys})
	if err != nil {
		return "", err
	}
	values, err := rs.States(ctx, room.StateFilter{Rooms: keys, Limit: stateLimitPerRoom * len(keys)})
	if err != nil {
		return "", err
	}

	others, err := otherAgents(ctx, &roomContext{store: st, rooms: rs, here: here}, sess.Key())
	if err != nil {
		return "", err
	}

	// Everything already in the room counts as delivered: the briefing shows it.
	if len(entries) > 0 {
		if err := rs.Ack(ctx, sess.Key(), entries[len(entries)-1].ID); err != nil {
			return "", err
		}
	}
	return room.Briefing(here, entries, reviews, values, others), nil
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

func sessionKeyFrom(harness session.Harness, p hook.Payload) string {
	if p.SessionID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", harness, p.SessionID)
}
