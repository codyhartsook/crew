package usage

import "encoding/json"

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

func consumeClaudeLine(raw []byte, s *Snapshot) bool {
	var line claudeLine
	if err := json.Unmarshal(raw, &line); err != nil || line.Message.Usage == nil {
		return false
	}
	// Claude Code writes "<synthetic>" placeholders, such as API errors, with zero usage.
	if line.Message.Model == "<synthetic>" {
		return false
	}
	u := line.Message.Usage
	input := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	s.InputTokens += input
	s.CachedInputTokens += u.CacheReadInputTokens
	s.OutputTokens += u.OutputTokens
	// The newest assistant message is the one still holding the window.
	s.ContextUsed = int(input)
	if line.Message.Model != "" {
		s.Model = line.Message.Model
	}
	if w, ok := WindowOf(s.Model); ok {
		s.ContextWindow = w
	}
	return true
}
