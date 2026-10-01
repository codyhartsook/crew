// Package usage records a session's context and token use, read from local
// files: Claude's transcript and Codex's rollout (hook payloads carry none).
package usage

import "time"

// Snapshot is what one session has spent. Zero fields mean "not known", so a
// missing figure must never read as zero spend.
type Snapshot struct {
	Model string `json:"model,omitempty"`
	// ContextWindow is the model's capacity in tokens.
	ContextWindow int `json:"context_window,omitempty"`
	// ContextUsed is the latest request's input, which is what occupies the
	// window right now. It does not grow monotonically: a compaction drops it.
	ContextUsed int `json:"context_used,omitempty"`
	// InputTokens and OutputTokens are cumulative. CachedInputTokens is the part of
	// InputTokens served from cache, kept apart as it bills at a fraction of the rate.
	InputTokens       int64     `json:"input_tokens,omitempty"`
	CachedInputTokens int64     `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64     `json:"output_tokens,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

// Known reports whether there is anything worth recording.
func (s Snapshot) Known() bool {
	return s.ContextUsed > 0 || s.InputTokens > 0 || s.OutputTokens > 0
}

// Headroom is the share of the context window still free, from 0 to 1. The bool
// is false when the window or occupancy is unknown, so callers do not rank on a default.
func (s Snapshot) Headroom() (float64, bool) {
	if s.ContextWindow <= 0 || s.ContextUsed <= 0 {
		return 0, false
	}
	free := float64(s.ContextWindow-s.ContextUsed) / float64(s.ContextWindow)
	if free < 0 {
		return 0, true
	}
	return free, true
}

// windows are context capacities for models that do not report their own.
// Codex states model_context_window in its rollout, so only Claude needs this.
var windows = map[string]int{
	"claude-fable-5-1":  1_000_000,
	"claude-fable-5":    1_000_000,
	"claude-opus-5":     1_000_000,
	"claude-opus-4-8":   1_000_000,
	"claude-opus-4-7":   1_000_000,
	"claude-opus-4-6":   1_000_000,
	"claude-sonnet-5":   1_000_000,
	"claude-sonnet-4-6": 1_000_000,
	"claude-haiku-4-5":  200_000,
}

// WindowOf returns a model's context capacity, for harnesses that do not say.
func WindowOf(model string) (int, bool) {
	if w, ok := windows[model]; ok {
		return w, true
	}
	w, ok := windows[baseModel(model)]
	return w, ok
}

// baseModel strips a bracketed variant, so "claude-opus-5[1m]" prices as
// "claude-opus-5".
func baseModel(model string) string {
	for i, r := range model {
		if r == '[' {
			return model[:i]
		}
	}
	return model
}
