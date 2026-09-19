package hookcmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// flakyNotifyStore fails MarkNotified for one chosen id, so a test can force
// the partial-failure path without the real store ever actually failing.
type flakyNotifyStore struct {
	*sqlitestore.Store
	failID string
}

func (f *flakyNotifyStore) MarkNotified(ctx context.Context, id string) error {
	if id == f.failID {
		return errors.New("injected failure")
	}
	return f.Store.MarkNotified(ctx, id)
}

func TestDelegationNoticeSurfacesAFinishedDelegation(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	d := &delegation.Delegation{
		ID: "d1", Room: "/repo", Role: "tester", Harness: "codex", Requester: "codex:launcher",
		Prompt: "run the tests", Status: delegation.StatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateDelegation(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartDelegation(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteDelegation(ctx, "d1", `{"ok":true}`); err != nil {
		t.Fatal(err)
	}

	out, err := roomsFor(ctx, st, hook.EventPrompt, nil, "codex:launcher")
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if !strings.Contains(out, "d1") || !strings.Contains(out, "finished") {
		t.Errorf("output = %q, want a notice naming the finished delegation", out)
	}
}

func TestDelegationNoticeShowsOnceOnly(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	d := &delegation.Delegation{
		ID: "d1", Room: "/repo", Role: "tester", Harness: "codex", Requester: "codex:launcher",
		Prompt: "run the tests", Status: delegation.StatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateDelegation(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartDelegation(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteDelegation(ctx, "d1", "done"); err != nil {
		t.Fatal(err)
	}

	if _, err := roomsFor(ctx, st, hook.EventPrompt, nil, "codex:launcher"); err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	out, err := roomsFor(ctx, st, hook.EventPrompt, nil, "codex:launcher")
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if strings.Contains(out, "d1") {
		t.Errorf("second roomsFor = %q, want no repeat notice", out)
	}
}

// A failed delegation should notify too - the requester needs to know it
// did not just succeed silently forever.
func TestDelegationNoticeSurfacesAFailure(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	d := &delegation.Delegation{
		ID: "d1", Room: "/repo", Role: "tester", Harness: "codex", Requester: "codex:launcher",
		Prompt: "run the tests", Status: delegation.StatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateDelegation(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartDelegation(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := st.FailDelegation(ctx, "d1", "codex is not installed"); err != nil {
		t.Fatal(err)
	}

	out, err := roomsFor(ctx, st, hook.EventPrompt, nil, "codex:launcher")
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if !strings.Contains(out, "d1") {
		t.Errorf("output = %q, want a notice naming the failed delegation", out)
	}
}

// A MarkNotified failure for one delegation must not swallow the notice for
// another that succeeded in the same pass, and the one that failed must stay
// available to retry rather than being silently dropped forever.
func TestDelegationNoticeKeepsSuccessesWhenOneMarkNotifiedFails(t *testing.T) {
	ctx := context.Background()
	real, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer real.Close()
	st := &flakyNotifyStore{Store: real, failID: "bad"}

	now := time.Now().UTC()
	for _, id := range []string{"good", "bad"} {
		d := &delegation.Delegation{
			ID: id, Room: "/repo", Role: "tester", Harness: "codex", Requester: "codex:launcher",
			Prompt: "run the tests", Status: delegation.StatusPending, CreatedAt: now, UpdatedAt: now,
		}
		if err := st.CreateDelegation(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartDelegation(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := st.CompleteDelegation(ctx, id, "done"); err != nil {
			t.Fatal(err)
		}
	}

	out, err := roomsFor(ctx, st, hook.EventPrompt, nil, "codex:launcher")
	if err == nil {
		t.Fatal("want the injected MarkNotified error to surface")
	}
	if !strings.Contains(out, "good") {
		t.Errorf("output = %q, want the successfully-notified delegation included", out)
	}
	if strings.Contains(out, "bad") {
		t.Errorf("output = %q, want the delegation whose MarkNotified failed excluded", out)
	}

	got, err := real.GetDelegation(ctx, "bad")
	if err != nil {
		t.Fatal(err)
	}
	if got.Notified {
		t.Error("bad.Notified = true, want it to remain unnotified so it is retried")
	}
}

// The child is the only party that knows its own session key, so its
// SessionStart is what makes the delegation addressable.
func TestSessionStartRecordsTheDelegationChild(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	if err := st.CreateDelegation(ctx, &delegation.Delegation{
		ID: "d1", Room: "/repo", Role: "tester", Harness: "claude", Requester: "codex:launcher",
		Prompt: "run the tests", Status: delegation.StatusPending, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	t.Setenv(cmdutil.EnvDelegation, "d1")
	if err := recordDelegationChild(ctx, st, hook.EventStart, "claude:spawned"); err != nil {
		t.Fatalf("recordDelegationChild: %v", err)
	}
	got, err := st.GetDelegation(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Child != "claude:spawned" {
		t.Errorf("Child = %q, want claude:spawned", got.Child)
	}

	// An ordinary session carries no delegation, and must not touch one.
	t.Setenv(cmdutil.EnvDelegation, "")
	if err := recordDelegationChild(ctx, st, hook.EventStart, "claude:someone-else"); err != nil {
		t.Fatalf("recordDelegationChild outside a delegation: %v", err)
	}
	if got, _ := st.GetDelegation(ctx, "d1"); got.Child != "claude:spawned" {
		t.Errorf("Child = %q, want it left alone", got.Child)
	}
}

func TestPromptNoticeSurfacesAWaitingQuestion(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, err := st.Ask(ctx, "claude:role", "codex:launcher", "which config does the suite read?")
	if err != nil {
		t.Fatal(err)
	}

	out, err := roomsFor(ctx, st, hook.EventPrompt, nil, "codex:launcher")
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if !strings.Contains(out, "which config does the suite read?") || !strings.Contains(out, "crew answer") {
		t.Errorf("output = %q, want the question and how to answer it", out)
	}

	// Answered, the notice goes; nothing repeats a question already handled.
	if _, err := st.Answer(ctx, id, "testdata/config.yaml"); err != nil {
		t.Fatal(err)
	}
	out, err = roomsFor(ctx, st, hook.EventPrompt, nil, "codex:launcher")
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if strings.Contains(out, "which config") {
		t.Errorf("output = %q, want no notice once answered", out)
	}
}

// Another session's question is not this session's business.
func TestPromptNoticeIgnoresAnotherSessionsQuestion(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.Ask(ctx, "claude:role", "codex:launcher", "which config?"); err != nil {
		t.Fatal(err)
	}
	out, err := roomsFor(ctx, st, hook.EventPrompt, nil, "codex:bystander")
	if err != nil {
		t.Fatalf("roomsFor: %v", err)
	}
	if strings.Contains(out, "which config") {
		t.Errorf("output = %q, want nothing for a session nobody asked", out)
	}
}
