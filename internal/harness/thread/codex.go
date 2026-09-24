package thread

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// lockDir holds one flock-backed file per open codex thread.
const lockDir = "thread-writer-locks"

// CodexSource reports open codex threads from their writer locks, rooted at
// Home. A nil Home uses CODEX_HOME or the conventional ~/.codex, matching
// usage.codexHome.
type CodexSource struct {
	Home func() string
}

func (s CodexSource) Open() (map[string]Thread, error) {
	home := s.Home
	if home == nil {
		home = defaultHome
	}
	dir := filepath.Join(home(), lockDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read thread locks: %w", err)
	}

	open := map[string]Thread{}
	var rollouts map[string]string // built lazily, once per call
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".lock") {
			continue
		}
		if !probeLockHeld(filepath.Join(dir, name)) {
			continue
		}
		id := strings.TrimSuffix(name, ".lock")
		th := Thread{ID: id}
		if rollouts == nil {
			rollouts = indexRollouts(home())
		}
		if path, ok := rollouts[id]; ok {
			if meta, started, active, err := readRollout(path); err == nil {
				th.CWD = meta.CWD
				th.Source = meta.ThreadSource
				th.Originator = meta.Originator
				th.Path = path
				th.Started = started
				th.Active = active
			}
		}
		open[id] = th
	}
	return open, nil
}

// probeLockHeld reports whether path's flock is held by another process. An
// unreadable lock is not evidence of death, so any failure to probe it reads
// as held.
func probeLockHeld(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return true
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true // EWOULDBLOCK, or anything else: held
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// rolloutIDPattern pulls a rollout's own thread id from its filename: the
// standard 8-4-4-4-12 hex groups right before the extension. The timestamp
// earlier in the name never has this shape, so this is unambiguous.
var rolloutIDPattern = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)

// indexRollouts globs every rollout once and indexes it by the full thread id
// in its filename. A partial-id match would be genuinely ambiguous: two
// threads from the same terminal share their first UUID segment.
func indexRollouts(home string) map[string]string {
	matches, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return map[string]string{}
	}
	index := make(map[string]string, len(matches))
	for _, path := range matches {
		if m := rolloutIDPattern.FindStringSubmatch(filepath.Base(path)); m != nil {
			index[m[1]] = path
		}
	}
	return index
}

// rolloutMeta is the part of a rollout's session_meta line thread liveness needs.
type rolloutMeta struct {
	CWD          string `json:"cwd"`
	ThreadSource string `json:"thread_source"`
	Originator   string `json:"originator"`
	Timestamp    string `json:"timestamp"`
}

type rolloutLine struct {
	Timestamp string      `json:"timestamp"`
	Payload   rolloutMeta `json:"payload"`
}

// readRollout reads a thread's first line for cwd, thread_source, originator
// and start time, and stats the file for its last activity.
func readRollout(path string) (rolloutMeta, time.Time, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return rolloutMeta{}, time.Time{}, time.Time{}, err
	}
	defer f.Close()

	// The first line can carry a whole system prompt, far past the default
	// scanner buffer.
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var meta rolloutMeta
	var started time.Time
	if sc.Scan() {
		var line rolloutLine
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			return rolloutMeta{}, time.Time{}, time.Time{}, err
		}
		meta = line.Payload
		// The thread's own stamp is when it began; the line's is when the
		// record was appended, which is later.
		started = firstTime(meta.Timestamp, line.Timestamp)
	}
	if err := sc.Err(); err != nil {
		return rolloutMeta{}, time.Time{}, time.Time{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return rolloutMeta{}, time.Time{}, time.Time{}, err
	}
	return meta, started, info.ModTime(), nil
}

// firstTime returns the first stamp that parses, zero when none do.
func firstTime(stamps ...string) time.Time {
	for _, s := range stamps {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// defaultHome is where codex keeps its rollouts and locks, matching usage.codexHome.
func defaultHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}
