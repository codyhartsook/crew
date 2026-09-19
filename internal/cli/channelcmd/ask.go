package channelcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/channel"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// defaultWait fits inside the ten-minute spawn timeout, leaving the role time
// to report after giving up.
const defaultWait = 4 * time.Minute

// maxWait is the longest wait that still leaves the spawn time to report.
const maxWait = 8 * time.Minute

// pollInterval is how often a waiting role re-reads its request. Nothing can
// wake a headless spawn: Claude's notifier needs a control socket `claude -p`
// never opens, and Codex's queues onto an interactive thread, not an exec.
// A var so tests need not wait on it.
var pollInterval = 2 * time.Second

// gaveUpNote closes a request nobody is waiting on any more.
const gaveUpNote = "(unanswered: the asking role stopped waiting)"

func NewAsk(opts *cmdutil.Options) *cobra.Command {
	var wait time.Duration

	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask the agent that delegated this task for context, and wait for the answer",
		Long: `Only a delegated role can ask. The target is the delegation's requester,
derived from CREW_DELEGATION rather than named here.

Blocks until the answer arrives or the wait expires. An expired wait is not a
failure to hide: report what you could not determine.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if wait <= 0 || wait > maxWait {
				return fmt.Errorf("--wait must be above zero and at most %s", maxWait)
			}
			id := os.Getenv(cmdutil.EnvDelegation)
			if id == "" {
				return errors.New("only a delegated role can ask: " + cmdutil.EnvDelegation + " is not set")
			}
			st, cs, err := channelStore(opts)
			if err != nil {
				return err
			}
			defer st.Close()
			ds, ok := st.(store.DelegationStore)
			if !ok {
				return errors.New("this store does not support delegation")
			}

			d, err := ds.GetDelegation(cmd.Context(), id)
			if err != nil {
				return err
			}
			if d.Requester == "" {
				return fmt.Errorf("delegation %s records no requester to ask", id)
			}
			// A terminal delegation has nobody waiting on this role's result,
			// and nothing left to close what it asks.
			if d.Status == delegation.StatusDone || d.Status == delegation.StatusFailed {
				return fmt.Errorf("delegation %s has already ended (%s); nobody is waiting for an answer", id, d.Status)
			}
			from, to := channel.Asker(d.Child, d.ID), channel.Addr(d.Requester)
			requestID, err := cs.Ask(cmd.Context(), from, to, strings.Join(args, " "))
			if err != nil {
				return err
			}
			// The requester is live, so the broker can wake it.
			opts.SignalBroker()

			answer, err := await(cmd.Context(), cs, requestID, wait)
			if err != nil {
				if _, closeErr := cs.CloseRequests(cmd.Context(), from, gaveUpNote); closeErr != nil {
					return errors.Join(err, closeErr)
				}
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), answer)
			return nil
		},
	}

	cmd.Flags().DurationVar(&wait, "wait", defaultWait, "how long to wait for an answer before giving up")
	return cmd
}

// await polls until the request is answered or the wait runs out, checking
// once more past the deadline so a last-interval answer is not lost.
func await(ctx context.Context, cs store.ChannelStore, id int64, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)
	for {
		r, err := cs.GetRequest(ctx, id)
		if err != nil {
			return "", err
		}
		if !r.Open() {
			return r.Answer, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no answer within %s: report what you could not determine", wait)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
