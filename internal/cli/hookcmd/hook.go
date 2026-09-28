// Package hookcmd records session lifecycle events and injects room context.
package hookcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/skill"
	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// maxPayload bounds how much of stdin the hook will read.
const maxPayload = 1 << 20

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		harnessFlag string
		quiet       bool
	)

	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Record a session lifecycle event read from stdin",
		Hidden: true,
		Long: `Reads the hook payload as JSON on stdin, detects the git checkout and
any pooled worktree, and writes the session to the store.

Never fails the calling harness: errors go to ~/.multiplayer/hook.log and the
process exits 0. Set CREW_DEBUG=1 to log every invocation and payload.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := runHook(cmd, opts, harnessFlag); err != nil {
				logHookError(opts, err)
				if !quiet {
					fmt.Fprintln(cmd.ErrOrStderr(), "crew hook:", err)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&harnessFlag, "harness", "auto",
		"calling harness registered by crew, or auto")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "log errors only, nothing on stderr")
	return cmd
}

func runHook(cmd *cobra.Command, opts *cmdutil.Options, harnessFlag string) error {
	h, err := resolveHarness(harnessFlag)
	if err != nil {
		return err
	}

	// Read stdin once so the raw bytes can be logged before parsing: a hook
	// that did nothing looks identical to one never invoked.
	raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxPayload))
	if err != nil {
		return fmt.Errorf("read hook payload: %w", err)
	}
	if debugEnabled() {
		logHookLine(opts, fmt.Sprintf("invoked harness=%s payload=%s", h, compact(raw)))
	}

	payload, err := hook.ParsePayload(bytes.NewReader(raw))
	if err != nil {
		return err
	}

	st, err := opts.OpenStore()
	if err != nil {
		return err
	}
	defer st.Close()
	if debugEnabled() && payload.Event() == hook.EventStart {
		logMissingCapabilities(opts, st)
	}

	// The budget depends on the event and the harness: a session-end hook gets
	// far less time than a session-start one, and each harness caps it its own way.
	ctx, cancel := context.WithTimeout(cmd.Context(), hook.BudgetFor(h, payload))
	defer cancel()

	recorder := &hook.Recorder{Store: st, Detector: detect.New()}
	sess, recordErr := recorder.Record(ctx, h, payload)

	sessionKey := sessionKeyFrom(h, payload)
	recordUsage(ctx, st, h, payload, sessionKey)

	injected, roomErr := roomsFor(ctx, st, payload.Event(), sess, sessionKey)
	if payload.Event() == hook.EventStart {
		// Claude misses skills written during its startup, so start doesn't sync.
		if err := makeSkillsDir(sess); err != nil {
			logHookError(opts, fmt.Errorf("create skills dir: %w", err))
		}
	} else if err := syncRoles(ctx, st); err != nil {
		// After an end is recorded, so role skills match who is left.
		logHookError(opts, fmt.Errorf("sync role skills: %w", err))
	}
	if writeErr := writeContext(cmd.OutOrStdout(), payload.Event(), injected); writeErr != nil {
		roomErr = errors.Join(roomErr, writeErr)
	}
	return errors.Join(recordErr, roomErr)
}

// syncRoles runs outside the agent sandbox, so it can always write skills.
func syncRoles(ctx context.Context, st store.Store) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return skill.SyncRoles(ctx, home, st)
}

// makeSkillsDir creates the dir Claude watches for project skills mid-session.
func makeSkillsDir(sess *session.Session) error {
	if sess == nil || sess.Harness != session.HarnessClaude || sess.CWD == "" || len(room.For(sess.Place)) == 0 {
		return nil
	}
	return os.MkdirAll(skill.ClaudeDir(sess.CWD), 0o755)
}

func resolveHarness(flag string) (session.Harness, error) {
	if flag == "" || flag == "auto" {
		return harness.Detect(), nil
	}
	h := session.Harness(flag)
	if _, ok := harness.For(h); ok {
		return h, nil
	}
	return "", fmt.Errorf("unknown harness %q", flag)
}

// debugEnabled reports whether every invocation should be logged. An
// environment variable rather than a flag: a flag would live in the hook
// config, and editing that changes its hash, re-triggering Codex's trust
// prompt.
func debugEnabled() bool {
	v := os.Getenv(cmdutil.EnvDebug)
	return v != "" && v != "0" && v != "false"
}

// logMissingCapabilities notes which optional store interfaces are absent,
// so CREW_DEBUG=1 gives a reason instead of silent no-ops.
func logMissingCapabilities(opts *cmdutil.Options, st store.Store) {
	var missing []string
	if _, ok := st.(store.RoomStore); !ok {
		missing = append(missing, "rooms")
	}
	if len(missing) > 0 {
		logHookLine(opts, "store missing capabilities: "+strings.Join(missing, ", "))
	}
}

// compact collapses a payload onto one line so the log stays greppable.
func compact(raw []byte) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "(empty)"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(trimmed)); err != nil {
		return strings.Join(strings.Fields(trimmed), " ")
	}
	return buf.String()
}

// logHookError records a hook that could not do its job.
func logHookError(opts *cmdutil.Options, hookErr error) {
	logHookLine(opts, "error: "+hookErr.Error())
}

// logHookLine appends to the hook log, the only durable record of what the hook
// saw. Failing to log is not worth escalating.
func logHookLine(opts *cmdutil.Options, line string) {
	path, err := hookLogPath(opts)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
}

// hookLogPath puts the log beside the database so both live in one place.
func hookLogPath(opts *cmdutil.Options) (string, error) {
	db, err := opts.DBPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(db), "hook.log"), nil
}
