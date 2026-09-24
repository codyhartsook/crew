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
	// remove is public: retracting your own post is the agent's job, not
	// machinery, and hiding it only kept it out of help.
	for _, name := range []string{"init", "uninstall", "ls", "whoami", "dashboard", "room", "post", "resolve", "remove", "search", "docs", "roles", "memory", "delegate"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name || cmd.Hidden {
			t.Errorf("public command %q = (%v, %v)", name, cmd, err)
		}
	}
	for _, name := range []string{"publish", "unpublish", "open"} {
		cmd, _, err := root.Find([]string{"docs", name})
		if err != nil || cmd.Name() != name || cmd.Hidden {
			t.Errorf("docs subcommand %q = (%v, %v)", name, cmd, err)
		}
	}
	for _, name := range []string{"get", "inbox", "install", "join", "leave", "open", "pick", "promote", "rm", "publish", "review", "state", "stop", "unpublish", "version"} {
		if cmd, _, err := root.Find([]string{name}); err == nil && cmd.Name() == name {
			t.Errorf("removed command %q is still available", name)
		}
	}
	for _, name := range []string{"clear", "hook", "serve", "prune"} {
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
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "reader")

	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{ID: "reader", Harness: session.HarnessCodex, Status: session.StatusActive, Place: session.Place{CWD: cwd, Repo: loc.Repo}, StartedAt: time.Now(), LastSeen: time.Now()}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
	here := room.For(*loc)
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

func TestRoomCommandsRepairMembership(t *testing.T) {
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
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "reader")

	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{ID: "reader", Harness: session.HarnessCodex, Status: session.StatusActive, Place: session.Place{CWD: cwd, Repo: loc.Repo}, StartedAt: time.Now(), LastSeen: time.Now()}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
	here := room.For(*loc)
	if err := st.Join(ctx, &room.Membership{SessionKey: sess.Key(), Room: here[0].Key, Scope: here[0].Scope, JoinedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	foreign := &room.Entry{Room: "/somewhere-else", Scope: room.ScopeWorktree, Mode: room.ModeRequest, Author: "claude:writer", Body: "private request", CreatedAt: time.Now()}
	if err := st.Post(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := run(t, "search", "--all", "private"); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("search --all error = %v, want unknown flag", err)
	}
	if err := run(t, "resolve", strconv.FormatInt(foreign.ID, 10), "done"); err == nil || !strings.Contains(err.Error(), "not in this room") {
		t.Fatalf("resolve foreign entry error = %v, want room error", err)
	}
	if err := run(t, "room"); err != nil {
		t.Fatalf("room for joined agent = %v", err)
	}

	st, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Leave(ctx, sess.Key(), here[0].Key); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err := run(t, "room"); err != nil {
		t.Fatalf("room repairs missing membership = %v", err)
	}
	st, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if memberships, err := st.Rooms(ctx, sess.Key()); err != nil || len(memberships) == 0 {
		t.Fatalf("room membership after repair = (%v, %v), want current room", memberships, err)
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
	for _, name := range []string{"addr", "verbose", "headless"} {
		if f := init.Flags().Lookup(name); f == nil || f.Hidden {
			t.Errorf("init --%s = %v, want public flag", name, f)
		}
	}
	if f := init.Flags().Lookup("restart"); f != nil {
		t.Errorf("init --restart = %v, want removed", f)
	}
}

func TestDashboardRequiresInit(t *testing.T) {
	err := run(t, "dashboard", "--addr", "127.0.0.1:0", "--no-open")
	if err == nil || !strings.Contains(err.Error(), "crew init") {
		t.Errorf("dashboard error = %v, want init guidance", err)
	}
}

// An anchored folder is one room. Without the anchor every directory would be
// its own, so agents working the same project would never see each other.
func TestFolderRoomSpansSubdirectories(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", path)
	t.Setenv("CODEX_THREAD_ID", "folder-agent")

	t.Chdir(root)
	if err := run(t, "anchor"); err != nil {
		t.Fatalf("anchor: %v", err)
	}

	// The agent starts in the subdirectory, as an agent usually would.
	t.Chdir(sub)
	loc, err := detect.New().Detect(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Folder == nil {
		t.Fatal("Folder = nil, want the anchor from the subdirectory")
	}

	st, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{
		ID: "folder-agent", Harness: session.HarnessCodex, Status: session.StatusActive,
		Place: *loc, StartedAt: time.Now(), LastSeen: time.Now(),
	}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := run(t, "post", "note", "shared context"); err != nil {
		t.Fatalf("post from a subdirectory: %v", err)
	}
	// Nothing sits above a folder, so there is no repository room to post to.
	if err := run(t, "post", "--repo", "note", "durable"); err == nil {
		t.Error("post --repo in a folder should fail, there is no repository room")
	}

	st, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	entries, err := st.Entries(ctx, room.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(entries), entries)
	}
	if entries[0].Room != loc.Folder.Root {
		t.Errorf("entry filed in %q, want the anchor %q", entries[0].Room, loc.Folder.Root)
	}
	if entries[0].Scope != room.ScopeFolder {
		t.Errorf("scope = %q, want %q", entries[0].Scope, room.ScopeFolder)
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
