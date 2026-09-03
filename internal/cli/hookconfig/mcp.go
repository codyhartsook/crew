package hookconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/codyhartsook/multiplayer/internal/backup"
)

// UnregisterLegacyChannel removes the preview channel installed by older init
// runs without touching another MCP server using the same name.
func UnregisterLegacyChannel(path string, dryRun bool) (bool, error) {
	original, config, err := Read(path)
	if err != nil {
		return false, err
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok || !legacyChannel(servers["multiplayer"]) {
		return false, nil
	}
	delete(servers, "multiplayer")
	if len(servers) == 0 {
		delete(config, "mcpServers")
	}
	updated, err := Encode(config)
	if err != nil {
		return false, err
	}
	if dryRun {
		return true, nil
	}
	if err := backup.Save(path, original); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	return true, os.WriteFile(path, updated, 0o644)
}

func legacyChannel(value any) bool {
	entry, ok := value.(map[string]any)
	if !ok {
		return false
	}
	args, ok := entry["args"].([]any)
	return ok && len(args) == 1 && args[0] == "channel"
}
