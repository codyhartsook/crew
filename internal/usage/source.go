package usage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Source opens a sampler for one harness session. Each harness owns how it
// locates and decodes its local usage data; callers only consume Snapshots.
type Source interface {
	Open(sessionID, transcriptPath string) Sampler
}

// Sampler incrementally reads one session's local usage data. changed is false
// when the source has not appended a new usage record since the last refresh.
type Sampler interface {
	Refresh(context.Context) (snapshot Snapshot, changed bool, err error)
}

// ClaudeSource reads the transcript path the Claude hook supplied.
type ClaudeSource struct{}

func (ClaudeSource) Open(_, transcriptPath string) Sampler {
	return newJSONLSampler(func() (string, error) {
		if transcriptPath == "" {
			return "", fmt.Errorf("claude usage needs a transcript path")
		}
		return transcriptPath, nil
	}, consumeClaudeLine)
}

// CodexSource reads rollouts rooted at Home. A nil Home uses CODEX_HOME or the
// conventional ~/.codex directory.
type CodexSource struct {
	Home func() string
}

func (s CodexSource) Open(sessionID, _ string) Sampler {
	home := s.Home
	if home == nil {
		home = codexHome
	}
	return newJSONLSampler(func() (string, error) {
		return FindCodexRollout(home(), sessionID)
	}, consumeCodexLine)
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

type lineConsumer func([]byte, *Snapshot) bool

// jsonlSampler keeps its read offset and any unfinished trailing line, so a
// long-running session reads only appended rollout/transcript data.
type jsonlSampler struct {
	locate  func() (string, error)
	consume lineConsumer

	path     string
	offset   int64
	pending  []byte
	snapshot Snapshot
}

func newJSONLSampler(locate func() (string, error), consume lineConsumer) *jsonlSampler {
	return &jsonlSampler{locate: locate, consume: consume}
}

func (s *jsonlSampler) Refresh(ctx context.Context) (Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	if s.path == "" {
		path, err := s.locate()
		if err != nil {
			return Snapshot{}, false, err
		}
		s.path = path
	}

	info, err := os.Stat(s.path)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("stat usage source: %w", err)
	}
	if info.Size() < s.offset {
		s.offset = 0
		s.pending = nil
		s.snapshot = Snapshot{}
	}
	if info.Size() == s.offset {
		return s.snapshot, false, nil
	}

	f, err := os.Open(s.path)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("open usage source: %w", err)
	}
	defer f.Close()
	if _, err := f.Seek(s.offset, io.SeekStart); err != nil {
		return Snapshot{}, false, fmt.Errorf("seek usage source: %w", err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("read usage source: %w", err)
	}
	s.offset += int64(len(data))
	data = append(s.pending, data...)

	changed := false
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		if s.consume(data[:i], &s.snapshot) {
			changed = true
		}
		data = data[i+1:]
	}
	s.pending = append(s.pending[:0], data...)
	if changed {
		s.snapshot.UpdatedAt = time.Now().UTC()
	}
	return s.snapshot, changed, nil
}
