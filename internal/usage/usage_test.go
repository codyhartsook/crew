package usage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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

func TestFindCodexRolloutPrefersExactIDAndRejectsAmbiguity(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "09", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "01a05f19-7831-78a1-9f95-11cd7a51f258"
	exact := filepath.Join(dir, "rollout-2026-09-02T13-00-13-"+id+".jsonl")
	other := filepath.Join(dir, "rollout-2026-09-02T13-00-14-01a05f19-9999-7b62-be69-92260048f107.jsonl")
	for _, path := range []string{exact, other} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := FindCodexRollout(home, id); err != nil || got != exact {
		t.Errorf("FindCodexRollout exact = (%q, %v), want %q", got, err, exact)
	}
	if _, err := FindCodexRollout(home, "01a05f19-0000-0000-0000-000000000000"); err == nil {
		t.Error("FindCodexRollout accepted ambiguous prefix")
	}
}

func TestClaudeSamplerReadsOnlyAppendedUsage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	first := `{"message":{"model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}` + "\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	sampler := (ClaudeSource{}).Open("session", path)
	got, changed, err := sampler.Refresh(context.Background())
	if err != nil || !changed {
		t.Fatalf("first Refresh = (%+v, %v, %v), want changed snapshot", got, changed, err)
	}
	if got.ContextUsed != 17 || got.InputTokens != 17 || got.OutputTokens != 2 || got.ContextWindow != 1_000_000 {
		t.Errorf("first snapshot = %+v", got)
	}
	if _, changed, err := sampler.Refresh(context.Background()); err != nil || changed {
		t.Errorf("unchanged Refresh changed=%v err=%v, want false nil", changed, err)
	}
	second := `{"message":{"model":"claude-opus-5","usage":{"input_tokens":20,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(second); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, changed, err = sampler.Refresh(context.Background())
	if err != nil || !changed {
		t.Fatalf("appended Refresh = (%+v, %v, %v), want changed snapshot", got, changed, err)
	}
	if got.ContextUsed != 20 || got.InputTokens != 37 || got.OutputTokens != 7 {
		t.Errorf("appended snapshot = %+v", got)
	}
}
