package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// claudeLine is the part of a transcript entry we read. Claude Code appends one
// JSON object per message; assistant messages carry a usage block.
type claudeLine struct {
	Message struct {
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// FromClaudeTranscript reads a session's spend from its transcript, whose path
// the hook payload carries.
//
// Context occupancy is the last request's whole input, cache included: a cache
// read still fills the window. Cumulative input counts the same way, so it
// double-counts history resent each turn, which is what the billed figure does
// too.
func FromClaudeTranscript(path string) (Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read transcript: %w", err)
	}
	defer f.Close()

	var s Snapshot
	sc := bufio.NewScanner(f)
	// Transcript lines carry whole tool results and can be far past the default
	// 64KB. A line we cannot buffer is skipped, not an error.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var line claudeLine
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue
		}
		u := line.Message.Usage
		if u == nil {
			continue
		}
		input := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
		s.InputTokens += input
		s.CachedInputTokens += u.CacheReadInputTokens
		s.OutputTokens += u.OutputTokens
		// The newest assistant message is the one still holding the window.
		s.ContextUsed = int(input)
		if line.Message.Model != "" {
			s.Model = line.Message.Model
		}
	}
	if err := sc.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("scan transcript: %w", err)
	}
	if w, ok := WindowOf(s.Model); ok {
		s.ContextWindow = w
	}
	return s, nil
}
