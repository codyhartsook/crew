package hookcmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/detect"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func sendHook(t *testing.T, opts *cmdutil.Options, event, id, cwd string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"session_id": id, "cwd": cwd, "hook_event_name": event})
	if err != nil {
		t.Fatal(err)
	}
	cmd := New(opts)
	cmd.SetArgs([]string{"--harness", "claude", "--quiet"})
	cmd.SetIn(strings.NewReader(string(payload)))
	cmd.SetOut(new(strings.Builder))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

// Claude misses a skill written during its startup, so SessionStart only makes the dir.
func TestSessionStartMakesSkillsDirWithoutSync(t *testing.T) {
	ctx := context.Background()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	opts := &cmdutil.Options{DB: filepath.Join(t.TempDir(), "sessions.db")}

	// A role already held here, so a sync would have something to write.
	st, err := sqlitestore.Open(opts.DB)
	if err != nil {
		t.Fatal(err)
	}
	place, err := detect.New().Detect(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	holder := &session.Session{ID: "holder", Harness: session.HarnessCodex, Status: session.StatusActive, Place: *place, StartedAt: now, LastSeen: now}
	if err := st.Upsert(ctx, holder); err != nil {
		t.Fatal(err)
	}
	if err := st.Assign(ctx, &store.Role{SessionKey: holder.Key(), Room: place.Repo.Root, Name: "tester", Description: "Runs the suite.", AssignedAt: now}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	sendHook(t, opts, "SessionStart", "arriving", repo)
	dir := filepath.Join(repo, ".claude", "skills")
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("skills dir after SessionStart = (%v, %v), want it empty", entries, err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents")); !os.IsNotExist(err) {
		t.Errorf("SessionStart synced role skills: %v", err)
	}

	sendHook(t, opts, "UserPromptSubmit", "arriving", repo)
	if _, err := os.Stat(filepath.Join(dir, "crew-role-"+holder.Alias, "SKILL.md")); err != nil {
		t.Errorf("first prompt did not sync the role skill: %v", err)
	}
}
