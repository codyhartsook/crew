// Package hook turns a harness session-lifecycle hook into a store record.
//
// Claude Code and Codex share a hook wire format, which is why one binary
// serves both; the hook configuration names which harness is calling.
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/proc"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// Budgets for a hook run. Only the start path does git work; the others run
// while the user waits.
const (
	StartBudget = 10 * time.Second
	// DefaultEndBudget applies to a harness this build does not recognize, so it
	// stays at the tightest of the known caps rather than overrunning one.
	DefaultEndBudget = 2500 * time.Millisecond

	// orphanDetectBudget caps location detection on the one end-path that needs
	// it, leaving room in the budget for the write that follows.
	orphanDetectBudget = 1 * time.Second
)

// Event is a normalized session-lifecycle event.
type Event string

const (
	EventStart Event = "SessionStart"
	EventEnd   Event = "SessionEnd"
	// EventPrompt is a user turn, where undelivered entries are handed over.
	EventPrompt Event = "UserPromptSubmit"
	// EventOther covers hook events the registry does not act on. Wiring an
	// unrelated event to this binary should be inert, not an error.
	EventOther Event = "other"
)

// Payload is the JSON a harness writes to a hook's stdin: the union of what
// Claude Code and Codex send, each omitting what it does not have.
type Payload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`

	// Source is Claude Code's SessionStart cause (startup, resume, clear,
	// compact); Trigger is the Codex spelling of the same idea.
	Source  string `json:"source"`
	Trigger string `json:"trigger"`
	// Reason is the SessionEnd cause.
	Reason string `json:"reason"`

	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
	AgentType      string `json:"agent_type"`
	TurnID         string `json:"turn_id"`
}

// ParsePayload reads a hook payload from r. Empty input is not an error: it
// yields a zero payload and Record falls back to the environment, so a harness
// that sends nothing cannot break the session.
func ParsePayload(r io.Reader) (Payload, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return Payload{}, fmt.Errorf("read hook payload: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return Payload{}, nil
	}
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return Payload{}, fmt.Errorf("decode hook payload: %w", err)
	}
	return p, nil
}

// Event normalizes the payload's event name. Both harnesses send PascalCase on
// the wire while naming the same events in snake_case in configuration, so both
// spellings are accepted.
func (p Payload) Event() Event {
	switch strings.ToLower(strings.ReplaceAll(p.HookEventName, "_", "")) {
	case "sessionstart":
		return EventStart
	case "sessionend":
		return EventEnd
	case "userpromptsubmit":
		return EventPrompt
	default:
		return EventOther
	}
}

// BudgetFor is how long the hook may take to handle this payload. Only the start
// path does git work; the others run in front of the user every turn, and each
// harness caps the session-end hook differently.
func BudgetFor(h session.Harness, p Payload) time.Duration {
	if p.Event() == EventStart {
		return StartBudget
	}
	if spec, ok := harness.For(h); ok {
		return spec.EndBudget
	}
	return DefaultEndBudget
}

// cause is why the session started or ended, whichever the harness supplied.
func (p Payload) cause() string {
	for _, v := range []string{p.Reason, p.Source, p.Trigger} {
		if v != "" {
			return v
		}
	}
	return ""
}

// Recorder writes session lifecycle events into a store.
type Recorder struct {
	Store    store.Store
	Detector detect.Detector
	// Now defaults to time.Now when nil.
	Now func() time.Time
}

// Record applies one hook payload and returns the session it recorded. It
// returns a nil session for events the registry does not track.
func (r *Recorder) Record(ctx context.Context, harness session.Harness, p Payload) (*session.Session, error) {
	if r.Store == nil {
		return nil, errors.New("recorder has no store")
	}

	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	at := now().UTC()

	switch p.Event() {
	case EventStart:
		return r.recordStart(ctx, harness, p, at)
	case EventEnd:
		return r.recordEnd(ctx, harness, p, at)
	case EventPrompt:
		// A turn changes nothing about where the session is; the CLI handles
		// room delivery for it.
		return nil, nil
	default:
		return nil, nil
	}
}

// recordStart registers a session and where it is working.
func (r *Recorder) recordStart(ctx context.Context, harness session.Harness, p Payload, at time.Time) (*session.Session, error) {
	sess := newSession(harness, p, at)

	// Location detection is best effort. A session outside a git checkout, or
	// one whose git call fails, is still worth recording.
	loc, detectErr := r.detect(ctx, p.CWD)
	applyLocation(sess, loc)

	return sess, errors.Join(r.Store.Upsert(ctx, sess), detectErr)
}

// recordEnd closes out a session. The common case touches no git: only the key
// is needed. Detection runs only for a session the store never saw - hook
// installed mid-session, or store reset - recorded already-ended.
func (r *Recorder) recordEnd(ctx context.Context, harness session.Harness, p Payload, at time.Time) (*session.Session, error) {
	sess := newSession(harness, p, at)
	sess.Status = session.StatusEnded
	sess.EndedAt = &at
	sess.EndReason = p.cause()

	err := r.Store.End(ctx, sess.Key(), at, sess.EndReason)
	if err == nil {
		return sess, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return sess, err
	}

	detectCtx, cancel := context.WithTimeout(ctx, orphanDetectBudget)
	defer cancel()
	loc, detectErr := r.detect(detectCtx, p.CWD)
	applyLocation(sess, loc)

	return sess, errors.Join(r.Store.Upsert(ctx, sess), detectErr)
}

// newSession builds the record common to both lifecycle events.
func newSession(harness session.Harness, p Payload, at time.Time) *session.Session {
	return &session.Session{
		ID:        sessionID(p),
		Harness:   harness,
		Status:    session.StatusActive,
		PID:       harnessPID(harness),
		Host:      detect.Hostname(),
		User:      username(),
		StartedAt: at,
		LastSeen:  at,
		Meta:      metaFor(p),
	}
}

func applyLocation(sess *session.Session, place *session.Place) {
	if place == nil {
		return
	}
	sess.Place = *place
}

// detect resolves the session's location, preferring the cwd the harness
// reported and falling back to the hook process's own working directory, which
// the harness sets to the session root.
func (r *Recorder) detect(ctx context.Context, payloadCWD string) (*session.Place, error) {
	cwd := payloadCWD
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve working directory: %w", err)
		}
		cwd = wd
	}
	d := r.Detector
	if d == nil {
		d = detect.New()
	}
	return d.Detect(ctx, cwd)
}

// harnessPID identifies the harness process this hook belongs to. The parent is
// normally it, but a harness that spawns hooks through a shell would record a
// pid that exits at once - indistinguishable from a dead session when reaping.
func harnessPID(h session.Harness) int {
	parent := os.Getppid()
	table, err := proc.Snapshot()
	if err != nil {
		return parent
	}
	// The process is recognized by its binary name, which the registry key is
	// not required to match.
	binary := string(h)
	if spec, ok := harness.For(h); ok {
		binary = spec.Binary
	}
	if table.Running(parent, binary) {
		return parent
	}
	if match := table.NearestMatch(os.Getpid(), binary); match != 0 {
		return match
	}
	return parent
}

// sessionID falls back to the harness process id when the payload carries no
// session id, so a session is still tracked rather than silently dropped.
func sessionID(p Payload) string {
	if p.SessionID != "" {
		return p.SessionID
	}
	return fmt.Sprintf("pid-%d", os.Getppid())
}

func metaFor(p Payload) map[string]string {
	meta := map[string]string{}
	for k, v := range map[string]string{
		"cause":           p.cause(),
		"model":           p.Model,
		"permission_mode": p.PermissionMode,
		"agent_type":      p.AgentType,
		"transcript_path": p.TranscriptPath,
	} {
		if v != "" {
			meta[k] = v
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}
