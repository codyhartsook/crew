package hookcmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
