package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// nonMemberSetup seeds a session that is in this location's room but has not
// joined it, plus one entry here and one in an unrelated room. A delegated role
// spawned with CREW_AUTO_JOIN=0 is exactly this session.
func nonMemberSetup(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if out, err := runGit(t, repo, "init", "-q", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	t.Setenv("CREW_DB", dbPath)
	t.Setenv("CODEX_THREAD_ID", "role")
	t.Chdir(repo)
	seedActiveSession(t, dbPath, repo, "role")

	st, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	here := roomKeyFor(t, repo)
	for _, e := range []*room.Entry{
		{Room: here, Scope: room.ScopeWorktree, Mode: room.ModeNote, Author: "codex:other", Body: "secret-here", CreatedAt: time.Now().UTC()},
		{Room: "/elsewhere", Scope: room.ScopeWorktree, Mode: room.ModeNote, Author: "codex:other", Body: "secret-elsewhere", CreatedAt: time.Now().UTC()},
	} {
		if err := st.Post(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

// roomKeyFor is the key the room commands will resolve dir to, which is not
// dir itself once symlinks are resolved.
func roomKeyFor(t *testing.T, dir string) string {
	t.Helper()
	loc, err := detect.New().Detect(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	rooms := room.For(session.Place{CWD: loc.CWD, Repo: loc.Repo})
	if len(rooms) == 0 {
		t.Fatalf("no room for %s", dir)
	}
	return rooms[0].Key
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	root := New()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

// The regression: Accessible returns nothing for a non-member, and an empty
// room list means unfiltered in the store, so `crew room` used to hand back
// every entry in every room.
func TestRoomShowsNothingToANonMember(t *testing.T) {
	t.Setenv("CREW_AUTO_JOIN", "0")
	nonMemberSetup(t)

	out, err := runRoot(t, "room")
	if err != nil {
		t.Fatalf("room: %v\n%s", err, out)
	}
	for _, leaked := range []string{"secret-here", "secret-elsewhere"} {
		if strings.Contains(out, leaked) {
			t.Errorf("crew room leaked %q to a non-member: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "no rooms here") {
		t.Errorf("output = %q, want it to say the session is in no room here", out)
	}
}

func TestSearchFindsNothingForANonMember(t *testing.T) {
	t.Setenv("CREW_AUTO_JOIN", "0")
	nonMemberSetup(t)

	out, err := runRoot(t, "search", "secret")
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	for _, leaked := range []string{"secret-here", "secret-elsewhere"} {
		if strings.Contains(out, leaked) {
			t.Errorf("crew search leaked %q to a non-member: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "no rooms here") {
		t.Errorf("output = %q, want it to say the session is in no room here", out)
	}
}

// A member still reads its own room, so the fix is a membership check and not
// a blanket refusal.
func TestRoomStillShowsEntriesToAMember(t *testing.T) {
	nonMemberSetup(t)

	out, err := runRoot(t, "room")
	if err != nil {
		t.Fatalf("room: %v\n%s", err, out)
	}
	if !strings.Contains(out, "secret-here") {
		t.Errorf("output = %q, want the entry in this session's own room", out)
	}
	if strings.Contains(out, "secret-elsewhere") {
		t.Errorf("output = %q, want nothing from another room", out)
	}
}
