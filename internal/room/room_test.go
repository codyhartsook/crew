package room_test

import (
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
)

func TestKindAddressed(t *testing.T) {
	addressed := map[room.Mode]bool{
		room.ModeNote:    false,
		room.ModeRequest: true,
	}
	for kind, want := range addressed {
		if got := kind.Addressed(); got != want {
			t.Errorf("%s.Addressed() = %v, want %v", kind, got, want)
		}
		if !kind.Valid() {
			t.Errorf("%s should be valid", kind)
		}
	}
	if room.Mode("gossip").Valid() {
		t.Error("unknown kind reported valid")
	}
}

func TestForRooms(t *testing.T) {
	cases := []struct {
		name  string
		place session.Place
		want  []room.Scope
		first string
		label string
	}{
		{
			// A primary checkout is one place, so it must not be two rooms and
			// have everything said in it twice.
			name:  "primary checkout collapses to one room",
			place: session.Place{Repo: &session.Repo{Name: "widget", Root: "/src/widget", MainRoot: "/src/widget"}},
			want:  []room.Scope{room.ScopeWorktree},
			first: "/src/widget",
		},
		{
			name: "linked worktree also joins its repository",
			place: session.Place{
				Repo: &session.Repo{
					Name: "widget", Root: "/pool/widget-abc/3/widget",
					MainRoot: "/src/widget", IsWorktree: true,
				},
				Pool: &session.Pool{Manager: "treehouse", Name: "widget-abc", Slot: "3"},
			},
			want:  []room.Scope{room.ScopeWorktree, room.ScopeRepo},
			first: "/pool/widget-abc/3/widget",
			label: "widget/3",
		},
		{
			// A pooled worktree is a directory named after the repo, so its own
			// leaf would label it "widget/widget".
			name: "linked worktree without pool metadata falls back to its parent",
			place: session.Place{
				Repo: &session.Repo{
					Name: "widget", Root: "/elsewhere/feature-x/widget",
					MainRoot: "/src/widget", IsWorktree: true,
				},
			},
			want:  []room.Scope{room.ScopeWorktree, room.ScopeRepo},
			first: "/elsewhere/feature-x/widget",
			label: "widget/feature-x",
		},
		{
			name:  "unanchored, the directory is its own room",
			place: session.Place{CWD: "/tmp/scratch"},
			want:  []room.Scope{room.ScopeFolder},
			first: "/tmp/scratch",
		},
		{
			// The anchor, not the cwd, is the key: that is what lets a session
			// started in a subdirectory share the folder's room.
			name: "an anchored folder is keyed on its anchor",
			place: session.Place{
				CWD:    "/work/notes/deep/inside",
				Folder: &session.Folder{Name: "notes", Root: "/work/notes"},
			},
			want:  []room.Scope{room.ScopeFolder},
			first: "/work/notes",
			label: "notes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := room.For(tc.place)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d rooms, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, scope := range tc.want {
				if got[i].Scope != scope {
					t.Errorf("room %d scope = %q, want %q", i, got[i].Scope, scope)
				}
			}
			if got[0].Key != tc.first {
				t.Errorf("first room key = %q, want %q", got[0].Key, tc.first)
			}
			if tc.label != "" && got[0].Name != tc.label {
				t.Errorf("first room name = %q, want %q", got[0].Name, tc.label)
			}
		})
	}
}

func TestEntryOpen(t *testing.T) {
	now := time.Now()
	cases := map[string]struct {
		entry room.Entry
		want  bool
	}{
		"unanswered question": {room.Entry{Mode: room.ModeRequest, CreatedAt: now}, true},
		"answered question":   {room.Entry{Mode: room.ModeRequest, ResolvedBy: 9, CreatedAt: now}, false},
		"the answer itself":   {room.Entry{Mode: room.ModeRequest, Resolves: 4, CreatedAt: now}, false},
		"a decision":          {room.Entry{Mode: room.ModeNote, CreatedAt: now}, false},
	}
	for name, tc := range cases {
		if got := tc.entry.Open(); got != tc.want {
			t.Errorf("%s: Open() = %v, want %v", name, got, tc.want)
		}
	}
}

func TestBriefing(t *testing.T) {
	here := []room.Room{{Key: "/src/widget", Scope: room.ScopeWorktree, Name: "widget"}}
	now := time.Now()
	entries := []*room.Entry{
		{ID: 1, Room: "/src/widget", Mode: room.ModeNote, Author: "codex:abcdef123", Body: "chose sqlite", CreatedAt: now},
		{ID: 2, Room: "/src/widget", Mode: room.ModeRequest, Author: "codex:abcdef123", Body: "who owns retries?", ResolvedBy: 3, CreatedAt: now},
		{ID: 3, Room: "/src/widget", Mode: room.ModeRequest, Author: "claude:zzz", Body: "the gateway does", Resolves: 2, CreatedAt: now},
		{ID: 4, Room: "/src/widget", Mode: room.ModeRequest, Author: "claude:zzz", To: "codex:abcdef123", Body: "this leaks", CreatedAt: now},
	}

	out := room.Briefing(here, entries, []string{"moss-otter (just now)"}, room.Authors{
		"claude:zzz": "moss-otter", "codex:abcdef123": "blue-wren",
	})
	for _, want := range []string{"chose sqlite", "Open", "request to blue-wren", "this leaks", "Answered", "the gateway does", "moss-otter"} {
		if !strings.Contains(out, want) {
			t.Errorf("briefing is missing %q:\n%s", want, out)
		}
	}
	// A resolved question belongs under Answered with its reply, not Open.
	openIdx := strings.Index(out, "### Open")
	answeredIdx := strings.Index(out, "### Answered")
	if openIdx < 0 || answeredIdx < 0 || openIdx > answeredIdx {
		t.Errorf("expected Open before Answered:\n%s", out)
	}
	if strings.Contains(out[openIdx:answeredIdx], "who owns retries") {
		t.Errorf("answered question listed as open:\n%s", out)
	}
}

func TestBriefingEmpty(t *testing.T) {
	here := []room.Room{{Key: "/src/widget", Scope: room.ScopeWorktree, Name: "widget"}}
	if out := room.Briefing(here, nil, nil, nil); out != "" {
		t.Errorf("briefing on an empty room = %q, want empty", out)
	}
}

// A pooled slot's path is handed to the next lease, so the room must not be.
func TestForSeparatesWorktreeRoomsByLease(t *testing.T) {
	repo := &session.Repo{
		Name: "widget", Root: "/pool/widget-abc/3/widget",
		MainRoot: "/src/widget", IsWorktree: true,
	}
	slot := func(lease string) *session.Pool {
		return &session.Pool{Manager: "treehouse", Name: "widget-abc", Slot: "3", LeaseID: lease}
	}

	first := room.For(session.Place{Repo: repo, Pool: slot("aaa")})
	second := room.For(session.Place{Repo: repo, Pool: slot("bbb")})
	again := room.For(session.Place{Repo: repo, Pool: slot("aaa")})

	if first[0].Key == second[0].Key {
		t.Errorf("two leases of one slot share room key %q", first[0].Key)
	}
	if first[0].Key != again[0].Key {
		t.Errorf("same lease gave %q then %q, want one room", first[0].Key, again[0].Key)
	}
	// The repository room is shared across leases, which is the point of it.
	if first[1].Key != second[1].Key {
		t.Errorf("repo rooms differ: %q and %q", first[1].Key, second[1].Key)
	}
}

// Without a lease there is nothing to scope to, so the path stands alone.
func TestForKeysUnleasedWorktreeOnPathAlone(t *testing.T) {
	repo := &session.Repo{
		Name: "widget", Root: "/elsewhere/widget",
		MainRoot: "/src/widget", IsWorktree: true,
	}
	for _, pool := range []*session.Pool{nil, {Manager: "treehouse", Name: "widget-abc", Slot: "3"}} {
		got := room.For(session.Place{Repo: repo, Pool: pool})
		if got[0].Key != repo.Root {
			t.Errorf("Key = %q, want the path %q", got[0].Key, repo.Root)
		}
	}
}
