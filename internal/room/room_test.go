package room_test

import (
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
)

func TestKindAddressed(t *testing.T) {
	addressed := map[room.Kind]bool{
		room.KindDecision: false,
		room.KindFinding:  false,
		room.KindQuestion: true,
		room.KindHandoff:  true,
		room.KindReview:   true,
	}
	for kind, want := range addressed {
		if got := kind.Addressed(); got != want {
			t.Errorf("%s.Addressed() = %v, want %v", kind, got, want)
		}
		if !kind.Valid() {
			t.Errorf("%s should be valid", kind)
		}
	}
	if room.Kind("gossip").Valid() {
		t.Error("unknown kind reported valid")
	}
}

func TestForRooms(t *testing.T) {
	cases := []struct {
		name  string
		repo  *session.Repo
		pool  *session.Pool
		cwd   string
		want  []room.Scope
		first string
		label string
	}{
		{
			// A primary checkout is one place, so it must not be two rooms and
			// have everything said in it twice.
			name:  "primary checkout collapses to one room",
			repo:  &session.Repo{Name: "widget", Root: "/src/widget", MainRoot: "/src/widget"},
			want:  []room.Scope{room.ScopeWorktree},
			first: "/src/widget",
		},
		{
			name: "linked worktree also joins its repository",
			repo: &session.Repo{
				Name: "widget", Root: "/pool/widget-abc/3/widget",
				MainRoot: "/src/widget", IsWorktree: true,
			},
			pool:  &session.Pool{Manager: "treehouse", Name: "widget-abc", Slot: "3"},
			want:  []room.Scope{room.ScopeWorktree, room.ScopeRepo},
			first: "/pool/widget-abc/3/widget",
			label: "widget/3",
		},
		{
			// A pooled worktree is a directory named after the repo, so its own
			// leaf would label it "widget/widget".
			name: "linked worktree without pool metadata falls back to its parent",
			repo: &session.Repo{
				Name: "widget", Root: "/elsewhere/feature-x/widget",
				MainRoot: "/src/widget", IsWorktree: true,
			},
			want:  []room.Scope{room.ScopeWorktree, room.ScopeRepo},
			first: "/elsewhere/feature-x/widget",
			label: "widget/feature-x",
		},
		{
			name:  "outside a checkout the directory is the room",
			repo:  nil,
			cwd:   "/tmp/scratch",
			want:  []room.Scope{room.ScopeWorktree},
			first: "/tmp/scratch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := room.For(tc.repo, tc.pool, tc.cwd)
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
		"unanswered question": {room.Entry{Kind: room.KindQuestion, CreatedAt: now}, true},
		"answered question":   {room.Entry{Kind: room.KindQuestion, ResolvedBy: 9, CreatedAt: now}, false},
		"the answer itself":   {room.Entry{Kind: room.KindQuestion, Resolves: 4, CreatedAt: now}, false},
		"a decision":          {room.Entry{Kind: room.KindDecision, CreatedAt: now}, false},
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
		{ID: 1, Room: "/src/widget", Kind: room.KindDecision, Author: "codex:abcdef123", Body: "chose sqlite", CreatedAt: now},
		{ID: 2, Room: "/src/widget", Kind: room.KindQuestion, Author: "codex:abcdef123", Body: "who owns retries?", ResolvedBy: 3, CreatedAt: now},
		{ID: 3, Room: "/src/widget", Kind: room.KindQuestion, Author: "claude:zzz", Body: "the gateway does", Resolves: 2, CreatedAt: now},
		{ID: 4, Room: "/src/widget", Kind: room.KindReview, Author: "claude:zzz", To: "codex:abcdef123", Body: "this leaks", CreatedAt: now},
	}

	out := room.Briefing(here, entries, nil, []string{"moss-otter (just now)"}, room.Authors{
		"claude:zzz": "moss-otter", "codex:abcdef123": "blue-wren",
	})
	for _, want := range []string{"chose sqlite", "Open", "review to blue-wren", "this leaks", "Answered", "the gateway does", "moss-otter"} {
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
	if out := room.Briefing(here, nil, nil, nil, nil); out != "" {
		t.Errorf("briefing on an empty room = %q, want empty", out)
	}
	if out := room.Delivery(nil, nil); out != "" {
		t.Errorf("delivery with nothing new = %q, want empty", out)
	}
}

func TestValidKey(t *testing.T) {
	for _, bad := range []string{"", "   ", "two words", "tab\there", strings.Repeat("k", 500)} {
		if err := room.ValidKey(bad); err == nil {
			t.Errorf("ValidKey(%q) accepted it", bad)
		}
	}
	for _, ok := range []string{"status", "build/status", "a.b-c_d", "migration/2026/step-1"} {
		if err := room.ValidKey(ok); err != nil {
			t.Errorf("ValidKey(%q) = %v, want nil", ok, err)
		}
	}
}

// State is what an arriving agent most needs, so it leads the briefing.
func TestBriefingIncludesState(t *testing.T) {
	here := []room.Room{{Key: "/src/widget", Scope: room.ScopeWorktree, Name: "widget"}}
	now := time.Now()
	values := []*room.State{
		{Room: "/src/widget", Key: "build/status", Value: "green", Author: "codex:a", Revision: 3, UpdatedAt: now},
	}

	out := room.Briefing(here, nil, values, nil, nil)
	if !strings.Contains(out, "### State") || !strings.Contains(out, "build/status: green") {
		t.Errorf("briefing is missing state:\n%s", out)
	}
	// A room with state but no entries is still worth briefing.
	if strings.Contains(out, "### Decisions") {
		t.Errorf("briefing invented an empty section:\n%s", out)
	}
	if idx := strings.Index(out, "### State"); idx < 0 {
		t.Errorf("state section absent:\n%s", out)
	}
}

// A runbook belongs in state, but pushing it whole at every arriving agent
// costs more context than it is worth.
func TestBriefingNamesLongStateWithoutReproducingIt(t *testing.T) {
	here := []room.Room{{Key: "/src/widget", Scope: room.ScopeWorktree, Name: "widget"}}
	runbook := "1. drain the node pool\n2. helm upgrade\n3. rollout status\n4. smoke suite\n5. uncordon"
	values := []*room.State{
		{Room: "/src/widget", Key: "cluster/update", Value: runbook, Author: "codex:a", Revision: 1, UpdatedAt: time.Now()},
		{Room: "/src/widget", Key: "build/status", Value: "green", Author: "codex:a", Revision: 1, UpdatedAt: time.Now()},
	}

	out := room.Briefing(here, nil, values, nil, nil)
	if strings.Contains(out, "helm upgrade") {
		t.Errorf("the runbook body leaked into the briefing:\n%s", out)
	}
	for _, want := range []string{
		"cluster/update: 1. drain the node pool (5 lines)",
		"multiplayer state get cluster/update",
		// A short value is still worth stating outright.
		"build/status: green",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("briefing is missing %q:\n%s", want, out)
		}
	}
}

func TestStateLong(t *testing.T) {
	short := &room.State{Value: "green"}
	multiline := &room.State{Value: "one\ntwo"}
	wide := &room.State{Value: strings.Repeat("x", 200)}

	if short.Long() {
		t.Error("a short single-line value should inline")
	}
	if !multiline.Long() || !wide.Long() {
		t.Error("a multi-line or very wide value should not inline")
	}
	if got := multiline.Summary(); got != "one" {
		t.Errorf("Summary() = %q, want the first line", got)
	}
	if got := multiline.Lines(); got != 2 {
		t.Errorf("Lines() = %d, want 2", got)
	}
	if got := short.Lines(); got != 1 {
		t.Errorf("Lines() = %d, want 1", got)
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

	first := room.For(repo, slot("aaa"), "")
	second := room.For(repo, slot("bbb"), "")
	again := room.For(repo, slot("aaa"), "")

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
		got := room.For(repo, pool, "")
		if got[0].Key != repo.Root {
			t.Errorf("Key = %q, want the path %q", got[0].Key, repo.Root)
		}
	}
}
