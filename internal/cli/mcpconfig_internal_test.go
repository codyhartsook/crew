package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUnregisterLegacyChannel(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	seed := `{"numStartups":7,"mcpServers":{"multiplayer":{"command":"multiplayer","args":["channel"]},"other":{"command":"other"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := unregisterLegacyChannel(path, false)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	servers := config["mcpServers"].(map[string]any)
	if _, ok := servers["multiplayer"]; ok || servers["other"] == nil || config["numStartups"] != float64(7) {
		t.Fatalf("unrelated config changed: %s", raw)
	}
}

func TestUnregisterLegacyChannelLeavesForeignEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	seed := `{"mcpServers":{"multiplayer":{"command":"mine","args":["serve"]}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := unregisterLegacyChannel(path, false)
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}
