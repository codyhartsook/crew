package usage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const liveUsageEnv = "LIVE_USAGE_SAMPLERS"

// These tests exercise the installed CLIs and their real persisted data. They
// are opt-in because each one sends two small model requests.
func TestLiveCodexSamplerSeesResumedSession(t *testing.T) {
	model := liveModel(t, "LIVE_CODEX_MODEL")
	dir := t.TempDir()
	output := runLive(t, dir, "codex", "exec", "--json", "--ignore-user-config",
		"--model", model, "--sandbox", "read-only", "--skip-git-repo-check", "Reply exactly: OK.")
	threadID := codexThreadID(t, output)

	sam := (CodexSource{}).Open(threadID, "")
	first := sampleLive(t, sam)
	runLive(t, dir, "codex", "exec", "resume", "--json", "--ignore-user-config",
		"--model", model, "--skip-git-repo-check", threadID, "Reply exactly: AGAIN.")
	second := sampleLive(t, sam)
	if second.InputTokens <= first.InputTokens || second.ContextUsed <= 0 {
		t.Errorf("resumed Codex usage = %+v after %+v", second, first)
	}
}

func TestLiveClaudeSamplerSeesResumedSession(t *testing.T) {
	model := liveModel(t, "LIVE_CLAUDE_MODEL")
	dir := t.TempDir()
	sessionID := liveUUID(t)
	runLive(t, dir, "claude", "--print", "--model", model, "--session-id", sessionID,
		"--permission-mode", "plan", "--output-format", "json", "Reply exactly: OK. Do not use tools.")
	path := claudeTranscript(t, sessionID)

	sam := (ClaudeSource{}).Open(sessionID, path)
	first := sampleLive(t, sam)
	runLive(t, dir, "claude", "--print", "--model", model, "--resume", sessionID,
		"--permission-mode", "plan", "--output-format", "json", "Reply exactly: AGAIN. Do not use tools.")
	second := sampleLive(t, sam)
	if second.InputTokens <= first.InputTokens || second.ContextUsed <= 0 {
		t.Errorf("resumed Claude usage = %+v after %+v", second, first)
	}
}

func liveModel(t *testing.T, name string) string {
	t.Helper()
	if os.Getenv(liveUsageEnv) != "1" {
		t.Skipf("set %s=1 and %s to run real harness usage tests", liveUsageEnv, name)
	}
	if model := os.Getenv(name); model != "" {
		return model
	}
	t.Skipf("set %s to a small model available to this account", name)
	return ""
}

func runLive(t *testing.T, dir, binary string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s failed: %v", binary, err)
	}
	return stdout.String()
}

func sampleLive(t *testing.T, sampler Sampler) Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, changed, err := sampler.Refresh(context.Background())
		if err != nil {
			t.Fatalf("sample usage: %v", err)
		}
		if changed && snapshot.Known() {
			return snapshot
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("sampler did not observe a usage update")
	return Snapshot{}
}

func codexThreadID(t *testing.T, output string) string {
	t.Helper()
	for scanner := bufio.NewScanner(strings.NewReader(output)); scanner.Scan(); {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Type == "thread.started" && event.ThreadID != "" {
			return event.ThreadID
		}
	}
	t.Fatal("Codex did not report a thread id")
	return ""
}

func claudeTranscript(t *testing.T, sessionID string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var found string
	err = filepath.WalkDir(filepath.Join(home, ".claude", "projects"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), sessionID+".jsonl") {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("no Claude transcript for %s", sessionID)
	}
	return found
}

func liveUUID(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	hexID := hex.EncodeToString(raw[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexID[:8], hexID[8:12], hexID[12:16], hexID[16:20], hexID[20:])
}
