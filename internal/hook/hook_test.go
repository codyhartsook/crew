package hook_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func TestParsePayload(t *testing.T) {
	t.Run("full payload", func(t *testing.T) {
		const body = `{
			"session_id": "abc-123",
			"transcript_path": "/tmp/t.jsonl",
			"cwd": "/src/widget",
			"hook_event_name": "SessionStart",
			"source": "startup",
			"model": "claude-opus-5"
		}`
		p, err := hook.ParsePayload(strings.NewReader(body))
		if err != nil {
			t.Fatalf("ParsePayload: %v", err)
		}
		if p.SessionID != "abc-123" || p.CWD != "/src/widget" || p.Model != "claude-opus-5" {
			t.Errorf("payload = %+v, want the decoded fields", p)
		}
		if p.Event() != hook.EventStart {
			t.Errorf("Event() = %q, want %q", p.Event(), hook.EventStart)
		}
	})

	// A harness that sends nothing must not break the session, so empty input
	// yields a zero payload rather than an error.
	t.Run("empty input", func(t *testing.T) {
		for _, in := range []string{"", "   \n"} {
			p, err := hook.ParsePayload(strings.NewReader(in))
			if err != nil {
				t.Fatalf("ParsePayload(%q): %v", in, err)
			}
			if p.SessionID != "" {
				t.Errorf("SessionID = %q, want empty", p.SessionID)
			}
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		if _, err := hook.ParsePayload(strings.NewReader("{not json")); err == nil {
			t.Fatal("ParsePayload accepted malformed JSON")
		}
	})
}

// Both harnesses send PascalCase on the wire but name the same events in
// snake_case in their configuration files.
func TestPayloadEvent(t *testing.T) {
	cases := map[string]hook.Event{
		"SessionStart":       hook.EventStart,
		"session_start":      hook.EventStart,
		"SessionEnd":         hook.EventEnd,
		"session_end":        hook.EventEnd,
		"UserPromptSubmit":   hook.EventPrompt,
		"user_prompt_submit": hook.EventPrompt,
		"PreToolUse":         hook.EventOther,
		"":                   hook.EventOther,
	}
	for name, want := range cases {
		if got := (hook.Payload{HookEventName: name}).Event(); got != want {
			t.Errorf("Event(%q) = %q, want %q", name, got, want)
		}
	}
}

// fixedDetector stands in for the git and pool probes so recorder tests
// do not need a real checkout on disk. It counts calls, which is how the tests
// assert that the end path avoids git work it does not need.
type fixedDetector struct {
	loc   *detect.Location
	err   error
	calls atomic.Int32
}

func (f *fixedDetector) Detect(context.Context, string) (*detect.Location, error) {
	f.calls.Add(1)
	return f.loc, f.err
}

func poolLocation() *detect.Location {
	return &detect.Location{
		CWD: "/pool/widget-abc/3/widget",
		Repo: &session.Repo{
			Name: "widget", Root: "/pool/widget-abc/3/widget",
			MainRoot: "/src/widget", Detached: true, IsWorktree: true,
		},
		Pool: &session.Pool{
			Manager: "treehouse", Name: "widget-abc", Slot: "3", Root: "/pool/widget-abc/3/widget",
			Leased: true, LeaseHolder: "agent:x",
		},
	}
}

func newRecorder(t *testing.T, d detect.Detector, now time.Time) (*hook.Recorder, store.Store) {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &hook.Recorder{Store: st, Detector: d, Now: func() time.Time { return now }}, st
}

func TestRecordSessionStart(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	rec, st := newRecorder(t, &fixedDetector{loc: poolLocation()}, at)

	payload := hook.Payload{
		SessionID:     "abc-123",
		HookEventName: "SessionStart",
		Source:        "startup",
		CWD:           "/pool/widget-abc/3/widget",
		Model:         "gpt-5.6-terra",
	}
	got, err := rec.Record(ctx, session.HarnessCodex, payload)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got == nil {
		t.Fatal("Record returned no session")
	}

	stored, err := st.Get(ctx, "codex:abc-123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != session.StatusActive {
		t.Errorf("Status = %q, want %q", stored.Status, session.StatusActive)
	}
	if stored.Repo == nil || stored.Repo.Name != "widget" {
		t.Errorf("Repo = %+v, want the detected checkout", stored.Repo)
	}
	if stored.Pool == nil || stored.Pool.Slot != "3" {
		t.Errorf("Pool = %+v, want the detected pool slot", stored.Pool)
	}
	if !stored.StartedAt.Equal(at) {
		t.Errorf("StartedAt = %v, want %v", stored.StartedAt, at)
	}
	if stored.Meta["cause"] != "startup" {
		t.Errorf("Meta[cause] = %q, want %q", stored.Meta["cause"], "startup")
	}
	if stored.Meta["model"] != "gpt-5.6-terra" {
		t.Errorf("Meta[model] = %q, want %q", stored.Meta["model"], "gpt-5.6-terra")
	}
}

func TestRecordSessionEnd(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	rec, st := newRecorder(t, &fixedDetector{loc: poolLocation()}, start)

	if _, err := rec.Record(ctx, session.HarnessClaude, hook.Payload{
		SessionID: "abc-123", HookEventName: "SessionStart", Source: "startup",
	}); err != nil {
		t.Fatalf("Record start: %v", err)
	}

	end := start.Add(20 * time.Minute)
	rec.Now = func() time.Time { return end }
	if _, err := rec.Record(ctx, session.HarnessClaude, hook.Payload{
		SessionID: "abc-123", HookEventName: "SessionEnd", Reason: "exit",
	}); err != nil {
		t.Fatalf("Record end: %v", err)
	}

	stored, err := st.Get(ctx, "claude:abc-123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != session.StatusEnded {
		t.Errorf("Status = %q, want %q", stored.Status, session.StatusEnded)
	}
	if stored.EndedAt == nil || !stored.EndedAt.Equal(end) {
		t.Errorf("EndedAt = %v, want %v", stored.EndedAt, end)
	}
	if !stored.StartedAt.Equal(start) {
		t.Errorf("StartedAt = %v, want the original %v", stored.StartedAt, start)
	}
	if stored.EndReason != "exit" {
		t.Errorf("EndReason = %q, want %q", stored.EndReason, "exit")
	}
}

// Installing the hooks mid-session means the first event the registry ever sees
// for a session can be its end. That session should still be recorded.
func TestRecordSessionEndWithoutStart(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	rec, st := newRecorder(t, &fixedDetector{loc: poolLocation()}, at)

	if _, err := rec.Record(ctx, session.HarnessCodex, hook.Payload{
		SessionID: "orphan", HookEventName: "SessionEnd", Reason: "exit",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	stored, err := st.Get(ctx, "codex:orphan")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != session.StatusEnded {
		t.Errorf("Status = %q, want %q", stored.Status, session.StatusEnded)
	}
	if stored.EndedAt == nil {
		t.Error("EndedAt is nil, want the end time")
	}
}

func TestRecordIgnoresUntrackedEvents(t *testing.T) {
	ctx := context.Background()
	rec, st := newRecorder(t, &fixedDetector{loc: poolLocation()}, time.Now())

	got, err := rec.Record(ctx, session.HarnessCodex, hook.Payload{
		SessionID: "abc-123", HookEventName: "PreToolUse",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got != nil {
		t.Errorf("Record returned %+v, want nil for an untracked event", got)
	}
	if _, err := st.Get(ctx, "codex:abc-123"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an untracked event should write nothing; Get err = %v", err)
	}
}

// A checkout the detector cannot read should still produce a session record:
// knowing an agent is running matters more than knowing its branch.
func TestRecordSurvivesDetectionFailure(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	rec, st := newRecorder(t, &fixedDetector{
		loc: &detect.Location{CWD: "/somewhere"},
		err: errors.New("git exploded"),
	}, at)

	if _, err := rec.Record(ctx, session.HarnessClaude, hook.Payload{
		SessionID: "abc-123", HookEventName: "SessionStart", CWD: "/somewhere",
	}); err == nil {
		t.Fatal("Record should report the detection failure")
	}

	stored, err := st.Get(ctx, "claude:abc-123")
	if err != nil {
		t.Fatalf("the session should be recorded despite detection failing: %v", err)
	}
	if stored.CWD != "/somewhere" {
		t.Errorf("CWD = %q, want %q", stored.CWD, "/somewhere")
	}
	if stored.Repo != nil {
		t.Errorf("Repo = %+v, want nil when detection failed", stored.Repo)
	}
}

// A session-end hook gets far less time than a session-start one, and each
// harness caps it its own way.
func TestBudgetFor(t *testing.T) {
	for _, spec := range harness.Specs() {
		start := hook.BudgetFor(spec.Harness, hook.Payload{HookEventName: "SessionStart"})
		end := hook.BudgetFor(spec.Harness, hook.Payload{HookEventName: "SessionEnd"})

		if start != hook.StartBudget {
			t.Errorf("%s: start budget = %v, want %v", spec.Harness, start, hook.StartBudget)
		}
		if end != spec.EndBudget {
			t.Errorf("%s: end budget = %v, want its spec's %v", spec.Harness, end, spec.EndBudget)
		}
		if end >= start {
			t.Errorf("%s: end budget %v should be tighter than start %v", spec.Harness, end, start)
		}
	}
}

// An unrecognized harness must not be given more than the tightest known cap.
func TestBudgetForUnknownHarness(t *testing.T) {
	end := hook.BudgetFor(session.HarnessUnknown, hook.Payload{HookEventName: "SessionEnd"})
	if end != hook.DefaultEndBudget {
		t.Errorf("end budget = %v, want %v", end, hook.DefaultEndBudget)
	}
	for _, spec := range harness.Specs() {
		if end > spec.EndBudget {
			t.Errorf("default budget %v exceeds %s's cap %v", end, spec.Harness, spec.EndBudget)
		}
	}
}

// Closing a known session needs nothing but its key. Running git there would
// spend the whole end-hook budget on work whose result is thrown away.
func TestRecordSessionEndSkipsDetection(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	detector := &fixedDetector{loc: poolLocation()}
	rec, st := newRecorder(t, detector, at)

	if _, err := rec.Record(ctx, session.HarnessCodex, hook.Payload{
		SessionID: "abc-123", HookEventName: "SessionStart", Source: "startup",
	}); err != nil {
		t.Fatalf("Record start: %v", err)
	}
	afterStart := detector.calls.Load()
	if afterStart != 1 {
		t.Fatalf("detector called %d times on start, want 1", afterStart)
	}

	if _, err := rec.Record(ctx, session.HarnessCodex, hook.Payload{
		SessionID: "abc-123", HookEventName: "SessionEnd", Reason: "exit",
	}); err != nil {
		t.Fatalf("Record end: %v", err)
	}
	if got := detector.calls.Load(); got != afterStart {
		t.Errorf("detector called %d times after end, want it untouched at %d", got, afterStart)
	}

	// The session is still closed out correctly, and keeps the location the
	// start event recorded.
	stored, err := st.Get(ctx, "codex:abc-123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != session.StatusEnded {
		t.Errorf("Status = %q, want %q", stored.Status, session.StatusEnded)
	}
	if stored.Pool == nil || stored.Pool.Slot != "3" {
		t.Errorf("Pool = %+v, want the location kept from the start event", stored.Pool)
	}
}

// The one end path that does need detection is a session the store never saw.
func TestRecordSessionEndDetectsOnlyForOrphans(t *testing.T) {
	ctx := context.Background()
	detector := &fixedDetector{loc: poolLocation()}
	rec, _ := newRecorder(t, detector, time.Now())

	if _, err := rec.Record(ctx, session.HarnessCodex, hook.Payload{
		SessionID: "orphan", HookEventName: "SessionEnd", Reason: "exit",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := detector.calls.Load(); got != 1 {
		t.Errorf("detector called %d times, want 1 for an unknown session", got)
	}
}
