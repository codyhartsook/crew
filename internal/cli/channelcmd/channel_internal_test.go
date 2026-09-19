package channelcmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/channel"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

const requester = "codex:launcher"

// seed writes a running delegation whose child has a session key, the state a
// role is in when it runs crew ask.
func seed(t *testing.T) (*cmdutil.Options, *sqlitestore.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	now := time.Now().UTC()
	d := &delegation.Delegation{
		ID: "d1", Room: "/repo", Dir: "/repo", Role: "tester", Harness: "claude",
		Requester: requester, Child: "claude:role", Prompt: "run the tests",
		Status: delegation.StatusRunning, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateDelegation(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = 2 * time.Second })
	return &cmdutil.Options{DB: path}, st
}

func run(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), err
}

func TestAskRefusesOutsideADelegation(t *testing.T) {
	opts, _ := seed(t)
	t.Setenv(cmdutil.EnvDelegation, "")

	out, err := run(t, NewAsk(opts), "which config?")
	if err == nil {
		t.Fatalf("ask outside a delegation = nil, want an error\n%s", out)
	}
	if !strings.Contains(err.Error(), cmdutil.EnvDelegation) {
		t.Errorf("error = %v, want it to name %s", err, cmdutil.EnvDelegation)
	}
}

func TestAskReturnsTheAnswer(t *testing.T) {
	opts, st := seed(t)
	t.Setenv(cmdutil.EnvDelegation, "d1")

	// The requester answers as soon as the question lands.
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		for {
			open, err := st.Inbox(context.Background(), channel.Addr(requester))
			if err == nil && len(open) == 1 {
				_, _ = st.Answer(context.Background(), open[0].ID, "testdata/config.yaml")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	out, err := run(t, NewAsk(opts), "--wait", "10s", "which config?")
	<-answered
	if err != nil {
		t.Fatalf("ask: %v\n%s", err, out)
	}
	if !strings.Contains(out, "testdata/config.yaml") {
		t.Errorf("output = %q, want the answer on stdout", out)
	}
}

func TestAskGivesUpWithSomethingToReport(t *testing.T) {
	opts, st := seed(t)
	t.Setenv(cmdutil.EnvDelegation, "d1")

	out, err := run(t, NewAsk(opts), "--wait", "50ms", "which config?")
	if err == nil {
		t.Fatalf("ask with nobody answering = nil, want an error\n%s", out)
	}
	if !strings.Contains(err.Error(), "report what you could not determine") {
		t.Errorf("error = %v, want it to say what to do instead", err)
	}
	// Nothing is left open for the requester to be nagged about.
	open, err := st.Inbox(context.Background(), channel.Addr(requester))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Errorf("Inbox = %+v, want the abandoned question closed", open)
	}
}

func TestAskRejectsAWaitLongerThanTheSpawn(t *testing.T) {
	opts, _ := seed(t)
	t.Setenv(cmdutil.EnvDelegation, "d1")

	if _, err := run(t, NewAsk(opts), "--wait", "30m", "which config?"); err == nil {
		t.Fatal("a wait past the spawn timeout was accepted")
	}
}

func TestAnswerReleasesOnceAndReportsTheSecondTry(t *testing.T) {
	opts, st := seed(t)
	id, err := st.Ask(context.Background(), "claude:role", requester, "which config?")
	if err != nil {
		t.Fatal(err)
	}

	out, err := run(t, NewAnswer(opts), "1", "testdata/config.yaml")
	if err != nil {
		t.Fatalf("answer: %v\n%s", err, out)
	}
	if !strings.Contains(out, "answered") {
		t.Errorf("output = %q, want a confirmation", out)
	}
	got, err := st.GetRequest(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Open() || got.Answer != "testdata/config.yaml" {
		t.Fatalf("request = %+v, want it closed with the answer", got)
	}

	_, err = run(t, NewAnswer(opts), "1", "actually this one")
	if err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Errorf("second answer = %v, want it refused", err)
	}
}

// Asking after the delegation ended would leave a request nothing closes, and
// no reader for the answer.
func TestAskRefusesOnceTheDelegationEnded(t *testing.T) {
	opts, st := seed(t)
	t.Setenv(cmdutil.EnvDelegation, "d1")
	if err := st.FailDelegation(context.Background(), "d1", "spawn died"); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, NewAsk(opts), "which config?"); err == nil {
		t.Fatal("ask on an ended delegation = nil, want an error")
	}
	open, err := st.Inbox(context.Background(), channel.Addr(requester))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Errorf("Inbox = %+v, want nothing asked", open)
	}
}
