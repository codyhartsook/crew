package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/harness/claude"
)

func mcpServers(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	servers, _ := config["mcpServers"].(map[string]any)
	return servers
}

func TestRegisterMCPRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")

	changed, err := registerMCP(path, claude.Name, claude.Entry("/opt/bin/multiplayer"), claude.IsOurs, false)
	if err != nil || !changed {
		t.Fatalf("install: changed=%v err=%v", changed, err)
	}
	entry, ok := mcpServers(t, path)["multiplayer"].(map[string]any)
	if !ok {
		t.Fatal("channel not registered")
	}
	if entry["command"] != "/opt/bin/multiplayer" {
		t.Errorf("command = %v", entry["command"])
	}

	if changed, err := registerMCP(path, claude.Name, claude.Entry("/opt/bin/multiplayer"), claude.IsOurs, false); err != nil || changed {
		t.Errorf("reinstall not idempotent: changed=%v err=%v", changed, err)
	}

	if changed, err := unregisterMCP(path, claude.Name, claude.IsOurs, false); err != nil || !changed {
		t.Fatalf("remove: changed=%v err=%v", changed, err)
	}
	if _, ok := mcpServers(t, path)["multiplayer"]; ok {
		t.Error("channel still registered after remove")
	}
}

func TestRegisterMCPPreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	seed := `{"numStartups":7,"mcpServers":{"other":{"command":"x"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := registerMCP(path, claude.Name, claude.Entry("/opt/bin/multiplayer"), claude.IsOurs, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := unregisterMCP(path, claude.Name, claude.IsOurs, false); err != nil {
		t.Fatalf("remove: %v", err)
	}

	raw, _ := os.ReadFile(path)
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config["numStartups"] != float64(7) {
		t.Errorf("unrelated key lost: %v", config["numStartups"])
	}
	if _, ok := config["mcpServers"].(map[string]any)["other"]; !ok {
		t.Error("another server was removed")
	}
}

// A server we did not write must survive uninstall even under our own name.
func TestUnregisterMCPLeavesForeignEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	seed := `{"mcpServers":{"multiplayer":{"command":"mine","args":["serve"]}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := unregisterMCP(path, claude.Name, claude.IsOurs, false)
	if err != nil || changed {
		t.Fatalf("removed a foreign entry: changed=%v err=%v", changed, err)
	}
	if _, ok := mcpServers(t, path)["multiplayer"]; !ok {
		t.Error("foreign entry deleted")
	}
}

func TestRegisterMCPLeavesForeignEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	seed := `{"mcpServers":{"multiplayer":{"command":"mine","args":["serve"]}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := registerMCP(path, claude.Name, claude.Entry("/opt/bin/multiplayer"), claude.IsOurs, false); err == nil {
		t.Fatal("replaced a foreign entry")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != seed {
		t.Errorf("foreign entry changed to %s", raw)
	}
}

func TestRegisterMCPPreservesFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := registerMCP(path, claude.Name, claude.Entry("/opt/bin/multiplayer"), claude.IsOurs, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range append([]string{path}, backupFiles(t, path)...) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o666 {
			t.Errorf("%s mode = %o, want 666", path, info.Mode().Perm())
		}
	}
}

func TestRegisterMCPDryRunWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	changed, err := registerMCP(path, claude.Name, claude.Entry("/opt/bin/multiplayer"), claude.IsOurs, true)
	if err != nil || !changed {
		t.Fatalf("dry run: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("dry run created the file")
	}
}

func backupFiles(t *testing.T, path string) []string {
	t.Helper()
	files, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("backups = %v, want one", files)
	}
	return files
}
