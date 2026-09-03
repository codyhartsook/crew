// Package usage records how much context and money a session has spent, so
// work can be routed to the agent with the most headroom or the lowest cost.
//
// Neither harness puts usage in its hook payload, but both write it to a local
// file the hook can read: Claude to its transcript, Codex to its rollout.
package usage

import "time"

// Snapshot is what one session has spent. Zero fields mean "not known", which
// is normal: a harness may not report a figure, and a missing one must never
// read as zero spend.
type Snapshot struct {
	Model string `json:"model,omitempty"`
	// ContextWindow is the model's capacity in tokens.
	ContextWindow int `json:"context_window,omitempty"`
	// ContextUsed is the latest request's input, which is what occupies the
	// window right now. It does not grow monotonically: a compaction drops it.
	ContextUsed int `json:"context_used,omitempty"`
	// InputTokens and OutputTokens are cumulative over the session.
	// CachedInputTokens is the share of InputTokens served from cache, kept
	// apart because it is billed at a fraction of the input rate.
	InputTokens       int64     `json:"input_tokens,omitempty"`
	CachedInputTokens int64     `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64     `json:"output_tokens,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

// Known reports whether there is anything worth recording.
func (s Snapshot) Known() bool {
	return s.ContextUsed > 0 || s.InputTokens > 0 || s.OutputTokens > 0
}

// Headroom is the share of the context window still free, from 0 to 1. The
// second result is false when the window or its occupancy is unknown, so a
// caller ranks on what it actually measured rather than on a default.
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

// Price is what a model charges per million tokens.
type Price struct {
	Input  float64
	Output float64
}

// prices are Anthropic first-party API rates, cached 2026-06-24. Codex models
// are deliberately absent: quoting a rate we cannot cite would make a cost
// comparison look authoritative when it is a guess, so cost reads as unknown
// and selection falls back to headroom.
var prices = map[string]Price{
	"claude-fable-5-1":  {Input: 10, Output: 50},
	"claude-fable-5":    {Input: 10, Output: 50},
	"claude-opus-5":     {Input: 5, Output: 25},
	"claude-opus-4-8":   {Input: 5, Output: 25},
	"claude-opus-4-7":   {Input: 5, Output: 25},
	"claude-opus-4-6":   {Input: 5, Output: 25},
	"claude-sonnet-5":   {Input: 2, Output: 10},
	"claude-sonnet-4-6": {Input: 3, Output: 15},
	"claude-haiku-4-5":  {Input: 1, Output: 5},
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

// PriceOf returns a model's rates. Names may carry a suffix the table does not
// list, as Claude Code reports "claude-opus-5[1m]", so the base name is tried
// after an exact miss.
func PriceOf(model string) (Price, bool) {
	if p, ok := prices[model]; ok {
		return p, true
	}
	p, ok := prices[baseModel(model)]
	return p, ok
}

// WindowOf returns a model's context capacity, for harnesses that do not say.
func WindowOf(model string) (int, bool) {
	if w, ok := windows[model]; ok {
		return w, true
	}
	w, ok := windows[baseModel(model)]
	return w, ok
}

// Multiplier is a model's output rate relative to the cheapest model we price.
// It is what makes "send this to the cheapest agent" comparable across models.
//
// A dollar figure is deliberately not offered: most input on a long session is
// served from cache at a fraction of the input rate, and we have no citable
// cache-read rate per model, so a total would overstate spend several-fold.
func Multiplier(model string) (float64, bool) {
	p, ok := PriceOf(model)
	if !ok {
		return 0, false
	}
	cheapest := p.Output
	for _, other := range prices {
		if other.Output < cheapest {
			cheapest = other.Output
		}
	}
	if cheapest <= 0 {
		return 0, false
	}
	return p.Output / cheapest, true
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
