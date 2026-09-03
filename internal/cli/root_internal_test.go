package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func TestPublicCommands(t *testing.T) {
	root := New()
	for _, name := range []string{"init", "uninstall", "ls", "dashboard", "room", "post", "resolve", "search"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name || cmd.Hidden {
			t.Errorf("public command %q = (%v, %v)", name, cmd, err)
		}
	}
	for _, name := range []string{"get", "inbox", "install", "join", "leave", "me", "pick", "promote", "rm", "review", "state", "version", "whoami"} {
		if cmd, _, err := root.Find([]string{name}); err == nil && cmd.Name() == name {
			t.Errorf("removed command %q is still available", name)
		}
	}
	for _, name := range []string{"clear", "hook", "remove", "serve", "prune"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || !cmd.Hidden {
			t.Errorf("internal command %q = (%v, %v), want hidden", name, cmd, err)
		}
	}
}

func TestRoomAcknowledgesRequests(t *testing.T) {
	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	loc, err := detect.New().Detect(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("MULTIPLAYER_DB", path)
	t.Setenv("CODEX_THREAD_ID", "reader")

	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{ID: "reader", Harness: session.HarnessCodex, Status: session.StatusActive, CWD: cwd, Repo: loc.Repo, StartedAt: time.Now(), LastSeen: time.Now()}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
	here := room.For(loc.Repo, loc.Pool, cwd)
	for _, r := range here {
		if err := st.Join(ctx, &room.Membership{SessionKey: sess.Key(), Room: r.Key, Scope: r.Scope, JoinedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Post(ctx, &room.Entry{Room: here[0].Key, Scope: here[0].Scope, Mode: room.ModeRequest, Author: "claude:writer", Body: "please check", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := run(t, "room"); err != nil {
		t.Fatal(err)
	}
	st, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if unread, err := st.Unread(ctx, sess.Key()); err != nil || len(unread) != 0 {
		t.Fatalf("Unread after crew room = (%v, %v), want none", unread, err)
	}
	note := &room.Entry{Room: here[0].Key, Scope: here[0].Scope, Mode: room.ModeNote, Author: "claude:writer", Body: "context", CreatedAt: time.Now()}
	if err := st.Post(ctx, note); err != nil {
		t.Fatal(err)
	}
	if err := run(t, "resolve", strconv.FormatInt(note.ID, 10), "done"); err == nil || !strings.Contains(err.Error(), "not an open request") {
		t.Fatalf("resolve note error = %v, want open-request error", err)
	}
}

func TestDashboardAliasesAndInitBrokerFlags(t *testing.T) {
	root := New()
	cmd, _, err := root.Find([]string{"fleet"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name() != "dashboard" || cmd.Hidden {
		t.Errorf("fleet = %q, hidden=%v", cmd.Name(), cmd.Hidden)
	}
	if f := cmd.Flags().Lookup("wake"); f != nil {
		t.Errorf("dashboard --wake = %v, want removed", f)
	}
	init, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"addr", "verbose"} {
		if f := init.Flags().Lookup(name); f == nil || f.Hidden {
			t.Errorf("init --%s = %v, want public flag", name, f)
		}
	}
}

func TestDashboardRequiresInit(t *testing.T) {
	err := run(t, "dashboard", "--addr", "127.0.0.1:0", "--no-open")
	if err == nil || !strings.Contains(err.Error(), "crew init") {
		t.Errorf("dashboard error = %v, want init guidance", err)
	}
}

// run executes one command through the root, which is where the flags are
// parsed and so the only place a command behaves as it does in the terminal.
func run(t *testing.T, args ...string) error {
	t.Helper()
	root := New()
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}
