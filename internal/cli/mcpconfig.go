package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/codyhartsook/multiplayer/internal/backup"
)

// mcpRoot is the key holding a harness's MCP servers.
const mcpRoot = "mcpServers"

// registerMCP adds or updates our MCP server entry, leaving a user's same-name
// server alone. It reports whether anything changed.
func registerMCP(path, name string, entry map[string]any, ours func(any) bool, dryRun bool) (bool, error) {
	original, config, err := readConfig(path)
	if err != nil {
		return false, err
	}
	servers, err := mcpServerMap(config)
	if err != nil {
		return false, err
	}
	if existing, ok := servers[name]; ok && !ours(existing) {
		return false, fmt.Errorf("%s already has an MCP server named %q", path, name)
	}
	servers[name] = entry
	config[mcpRoot] = servers
	return writeConfig(path, original, config, dryRun)
}

// unregisterMCP removes an entry only when ours says we wrote it, so a server
// of the same name that someone else added survives.
func unregisterMCP(path, name string, ours func(any) bool, dryRun bool) (bool, error) {
	original, config, err := readConfig(path)
	if err != nil {
		return false, err
	}
	servers, err := mcpServerMap(config)
	if err != nil {
		return false, err
	}
	if !ours(servers[name]) {
		return false, nil
	}
	delete(servers, name)
	if len(servers) == 0 {
		delete(config, mcpRoot)
	} else {
		config[mcpRoot] = servers
	}
	return writeConfig(path, original, config, dryRun)
}

func mcpServerMap(config map[string]any) (map[string]any, error) {
	value, ok := config[mcpRoot]
	if !ok {
		return map[string]any{}, nil
	}
	servers, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", mcpRoot)
	}
	return servers, nil
}

func writeConfig(path string, original []byte, config map[string]any, dryRun bool) (bool, error) {
	updated, err := encodeConfig(config)
	if err != nil {
		return false, err
	}
	if string(original) == string(updated) {
		return false, nil
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
