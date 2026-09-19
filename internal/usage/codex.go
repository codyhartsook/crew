package usage

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// codexLine is the part of a rollout entry we read. Codex appends one JSON
// object per event; a token_count event states both totals and the window.
type codexLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Info  *struct {
			TotalTokenUsage codexTokens `json:"total_token_usage"`
			LastTokenUsage  codexTokens `json:"last_token_usage"`
			ContextWindow   int         `json:"model_context_window"`
		} `json:"info"`
	} `json:"payload"`
}

type codexTokens struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
}

func consumeCodexLine(raw []byte, s *Snapshot) bool {
	var line codexLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return false
	}
	// turn_context names the model; token_count carries the figures.
	if line.Type == "turn_context" && line.Payload.Model != "" {
		s.Model = line.Payload.Model
	}
	if line.Payload.Type != "token_count" || line.Payload.Info == nil {
		return false
	}
	info := line.Payload.Info
	s.InputTokens = info.TotalTokenUsage.InputTokens
	s.CachedInputTokens = info.TotalTokenUsage.CachedInputTokens
	s.OutputTokens = info.TotalTokenUsage.OutputTokens
	s.ContextWindow = info.ContextWindow
	// The last turn's input is what occupies the window.
	s.ContextUsed = int(info.LastTokenUsage.InputTokens)
	return true
}

// FindCodexRollout locates a thread's rollout under codexHome. Prefer an exact
// session id in the filename; older Codex rollouts may only share its prefix,
// which is safe only when that match is unique.
func FindCodexRollout(codexHome, threadID string) (string, error) {
	prefix, _, ok := strings.Cut(threadID, "-")
	if !ok || prefix == "" {
		return "", fmt.Errorf("thread id %q has no id prefix", threadID)
	}
	var matches []string
	err := filepath.WalkDir(filepath.Join(codexHome, "sessions"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil //nolint:nilerr // an unreadable subtree is not fatal
		}
		if strings.HasSuffix(d.Name(), "-"+threadID+".jsonl") {
			matches = []string{path}
			return fs.SkipAll
		}
		if strings.Contains(d.Name(), prefix) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search rollouts: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no rollout for thread %s", threadID)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous rollout for thread %s", threadID)
	}
	return matches[0], nil
}
