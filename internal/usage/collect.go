package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Harness names, as spelled by session.Harness. Kept as plain strings so this
// package stays a leaf: the session model holds a Snapshot, so it cannot be
// imported back.
const (
	claudeHarness = "claude"
	codexHarness  = "codex"
)

// For reads a session's spend from whichever local file its harness writes.
// transcript is the path the harness handed us, empty when it did not.
func For(harness, transcript, sessionID string) (Snapshot, error) {
	var (
		s   Snapshot
		err error
	)
	switch harness {
	case claudeHarness:
		if transcript == "" {
			return Snapshot{}, fmt.Errorf("claude usage needs a transcript path")
		}
		s, err = FromClaudeTranscript(transcript)
	case codexHarness:
		path, findErr := FindCodexRollout(codexHome(), sessionID)
		if findErr != nil {
			return Snapshot{}, findErr
		}
		s, err = FromCodexRollout(path)
	default:
		return Snapshot{}, fmt.Errorf("no usage source for harness %q", harness)
	}
	if err != nil {
		return Snapshot{}, err
	}
	s.UpdatedAt = time.Now().UTC()
	return s, nil
}

// codexHome is where Codex keeps its rollouts.
func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}
