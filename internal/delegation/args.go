package delegation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/role"
)

// defaultSandbox is used when a role's definition does not set one.
const defaultSandbox = "workspace-write"

// claudeArgs builds a headless Claude spawn.
func claudeArgs(def role.Definition, task string, schema []byte) []string {
	args := []string{"-p", task}
	if def.Instructions != "" {
		args = append(args, "--append-system-prompt", def.Instructions)
	}
	if def.Render.Model != "" {
		args = append(args, "--model", def.Render.Model)
	}
	if def.Render.Effort != "" {
		args = append(args, "--effort", def.Render.Effort)
	}
	if len(schema) > 0 {
		args = append(args, "--json-schema", string(schema))
	}
	args = append(args, "--permission-mode", "acceptEdits", "--permission-prompts", "none")
	if len(def.Render.Tools) > 0 {
		args = append(args, "--allowedTools", strings.Join(def.Render.Tools, ","))
	}
	args = append(args, "--output-format", "json")
	return args
}

// codexArgs builds a headless Codex spawn; codex exec has no system-prompt flag.
func codexArgs(def role.Definition, task, schemaPath, resultFile string) []string {
	prompt := task
	if def.Instructions != "" {
		prompt = def.Instructions + "\n\n" + task
	}
	args := []string{"exec", prompt}
	if def.Render.Model != "" {
		args = append(args, "-m", def.Render.Model)
	}
	sandbox := def.Render.Sandbox
	if sandbox == "" {
		sandbox = defaultSandbox
	}
	args = append(args, "-s", sandbox)
	if def.Render.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+def.Render.Effort)
	}
	if schemaPath != "" {
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args, "-o", resultFile)
	return args
}

// loadSchema reads a role's output schema, relative to its definition file.
// Codex requires additionalProperties: false; crew does not rewrite it in.
func loadSchema(def role.Definition) ([]byte, error) {
	if def.Output.Schema == "" {
		return nil, nil
	}
	path := def.Output.Schema
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(def.Path), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read output schema for role %q: %w", def.Name, err)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("output schema for role %q at %s is not valid JSON", def.Name, path)
	}
	return data, nil
}

// claudeResult is the envelope "claude -p --output-format json" writes.
type claudeResult struct {
	Type             string          `json:"type"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
}

// parseClaudeOutput extracts the bounded result from a completed spawn.
func parseClaudeOutput(raw []byte) (string, error) {
	var env claudeResult
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("parse claude result: %w", err)
	}
	if env.IsError {
		return "", fmt.Errorf("claude reported an error: %s", env.Result)
	}
	if len(env.StructuredOutput) > 0 && string(env.StructuredOutput) != "null" {
		return string(env.StructuredOutput), nil
	}
	return env.Result, nil
}
