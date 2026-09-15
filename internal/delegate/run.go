package delegate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// Run spawns one headless harness invocation in dir and returns its bounded
// result. env carries the caller's environment additions.
func Run(ctx context.Context, h session.Harness, def role.Definition, task, dir string, env []string) (string, error) {
	bin, err := exec.LookPath(string(h))
	if err != nil {
		return "", fmt.Errorf("%s is not installed or not on PATH", h)
	}
	schema, err := loadSchema(def)
	if err != nil {
		return "", err
	}
	fullEnv := append(os.Environ(), env...)

	switch h {
	case session.HarnessClaude:
		cmd := exec.CommandContext(ctx, bin, claudeArgs(def, task, schema)...)
		cmd.Dir = dir
		cmd.Env = fullEnv
		out, err := runCapturing(cmd)
		if err != nil {
			return "", err
		}
		return parseClaudeOutput(out)

	case session.HarnessCodex:
		resultFile, err := os.CreateTemp("", "crew-delegate-result-*.json")
		if err != nil {
			return "", fmt.Errorf("create result file: %w", err)
		}
		resultFile.Close()
		defer os.Remove(resultFile.Name())

		var schemaPath string
		if len(schema) > 0 {
			schemaFile, err := os.CreateTemp("", "crew-delegate-schema-*.json")
			if err != nil {
				return "", fmt.Errorf("create schema file: %w", err)
			}
			if _, err := schemaFile.Write(schema); err != nil {
				schemaFile.Close()
				return "", fmt.Errorf("write schema file: %w", err)
			}
			schemaFile.Close()
			schemaPath = schemaFile.Name()
			defer os.Remove(schemaPath)
		}

		cmd := exec.CommandContext(ctx, bin, codexArgs(def, task, schemaPath, resultFile.Name())...)
		cmd.Dir = dir
		cmd.Env = fullEnv
		if _, err := runCapturing(cmd); err != nil {
			return "", err
		}
		data, err := os.ReadFile(resultFile.Name())
		if err != nil {
			return "", fmt.Errorf("read result: %w", err)
		}
		return strings.TrimSpace(string(data)), nil

	default:
		return "", fmt.Errorf("unsupported harness %q", h)
	}
}

// runCapturing runs cmd to completion; Stdin nil means the null device.
func runCapturing(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("%s: %w: %s", filepath.Base(cmd.Path), err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("%s: %w", filepath.Base(cmd.Path), err)
	}
	return out, nil
}
