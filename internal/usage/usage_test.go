package usage

import (
	"os"
	"path/filepath"
	"testing"
)

// Context occupancy is the newest request's whole input, cache included: a
// cached token still fills the window. Totals accumulate over every message.
func TestFromClaudeTranscript(t *testing.T) {
	got, err := FromClaudeTranscript(filepath.Join("testdata", "claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		Model:             "claude-opus-5",
		ContextWindow:     1_000_000,
		ContextUsed:       1000, // 5 + 900 + 95, the last message only
		InputTokens:       1050, // (10+0+40) + (5+900+95)
		CachedInputTokens: 900,  // cache reads only
		OutputTokens:      300,  // 100 + 200
	}
	if got != want {
		t.Errorf("snapshot =\n %+v\nwant\n %+v", got, want)
	}
}

// A rollout states totals on every token_count event, so the last one wins
// rather than summing into a figure several times too large.
func TestFromCodexRollout(t *testing.T) {
	got, err := FromCodexRollout(filepath.Join("testdata", "codex.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		Model:             "gpt-x-codex",
		ContextWindow:     258400,
		ContextUsed:       1200,
		InputTokens:       3000,
		CachedInputTokens: 2500,
		OutputTokens:      90,
	}
	if got != want {
		t.Errorf("snapshot =\n %+v\nwant\n %+v", got, want)
	}
}

func TestMissingFileIsAnError(t *testing.T) {
	if _, err := FromClaudeTranscript("testdata/nope.jsonl"); err == nil {
		t.Error("a missing transcript should be an error")
	}
	if _, err := FromCodexRollout("testdata/nope.jsonl"); err == nil {
		t.Error("a missing rollout should be an error")
	}
}

// An unknown figure must never read as zero spend, or an agent with no usage
// recorded would look like the emptiest one and win every routing decision.
func TestHeadroomSaysWhenItCannotTell(t *testing.T) {
	cases := map[string]Snapshot{
		"no window": {ContextUsed: 100},
		"no usage":  {ContextWindow: 1000},
		"empty":     {},
	}
	for name, s := range cases {
		if _, ok := s.Headroom(); ok {
			t.Errorf("%s: Headroom reported a value it cannot know", name)
		}
	}
	s := Snapshot{ContextWindow: 1000, ContextUsed: 250}
	got, ok := s.Headroom()
	if !ok || got != 0.75 {
		t.Errorf("Headroom = (%v, %v), want (0.75, true)", got, ok)
	}
	// Occupancy past the window reads as full, not negative.
	over := Snapshot{ContextWindow: 1000, ContextUsed: 1200}
	if got, ok := over.Headroom(); !ok || got != 0 {
		t.Errorf("over-window Headroom = (%v, %v), want (0, true)", got, ok)
	}
}

func TestWindowAcceptsAVariantSuffix(t *testing.T) {
	if w, ok := WindowOf("claude-opus-5[1m]"); !ok || w != 1_000_000 {
		t.Errorf("WindowOf(variant) = (%d, %v), want 1000000", w, ok)
	}
}

// Codex names a rollout after a sibling uuid sharing only the first segment of
// the thread id, so the prefix is all there is to match on.
func TestFindCodexRollout(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "09", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "rollout-2026-09-02T13-00-13-01a05f19-7894-7b62-be69-92260048f107.jsonl")
	if err := os.WriteFile(want, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FindCodexRollout(home, "01a05f19-7831-78a1-9f95-11cd7a51f258")
	if err != nil || got != want {
		t.Errorf("FindCodexRollout = (%q, %v), want %q", got, err, want)
	}
	if _, err := FindCodexRollout(home, "01a99999-0000-0000-0000-000000000000"); err == nil {
		t.Error("an unknown thread should not resolve to a rollout")
	}
}
