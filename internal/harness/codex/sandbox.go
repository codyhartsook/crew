package codex

import (
	"fmt"
	"os"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/backup"
)

const sandboxSection = "[sandbox_workspace_write]"

// EnsureWritableRoot grants Codex's sandbox write access to dir. Codex runs
// agent commands under seatbelt with only the workspace writable, so without
// this every room write fails as a readonly database. Hooks are unsandboxed,
// which is why the breakage looks selective.
func EnsureWritableRoot(path, dir string, dryRun bool) (bool, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if dryRun {
				return true, nil
			}
			return true, os.WriteFile(path, []byte(newSection(dir)), 0o600)
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	updated, changed := addWritableRoot(string(original), dir)
	if !changed || dryRun {
		return changed, nil
	}
	if err := backup.Save(path, original); err != nil {
		return false, err
	}
	// Preserve the file's own permissions; it can hold credentials.
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// addWritableRoot edits the config text in place rather than round-tripping it
// through a TOML encoder, which would discard the comments and ordering the
// file was written with.
func addWritableRoot(content, dir string) (string, bool) {
	quoted := fmt.Sprintf("%q", dir)
	if strings.Contains(content, quoted) {
		return content, false
	}

	lines := strings.Split(content, "\n")
	section := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == sandboxSection {
			section = i
			break
		}
	}
	if section < 0 {
		return strings.TrimRight(content, "\n") + "\n\n" + newSection(dir), true
	}

	// Look for the key only within this section, so a writable_roots belonging
	// to some other table is never touched.
	for i := section + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		if !strings.HasPrefix(trimmed, "writable_roots") {
			continue
		}
		open := strings.Index(lines[i], "[")
		if open < 0 {
			continue
		}
		// Inserting just after the bracket works whether the array is written
		// on one line or spread over several.
		rest := strings.TrimSpace(lines[i][open+1:])
		separator := ", "
		if rest == "" || strings.HasPrefix(rest, "]") {
			separator = ""
		}
		lines[i] = lines[i][:open+1] + quoted + separator + lines[i][open+1:]
		return strings.Join(lines, "\n"), true
	}

	// The section exists without the key.
	inserted := append([]string{}, lines[:section+1]...)
	inserted = append(inserted, "writable_roots = ["+quoted+"]")
	inserted = append(inserted, lines[section+1:]...)
	return strings.Join(inserted, "\n"), true
}

// RemoveWritableRoot takes dir back out of Codex's sandbox roots, leaving the
// rest of the line, and the file, as they were.
func RemoveWritableRoot(path, dir string, dryRun bool) (bool, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	quoted := fmt.Sprintf("%q", dir)
	if !strings.Contains(string(original), quoted) {
		return false, nil
	}
	if dryRun {
		return true, nil
	}

	updated := string(original)
	for _, form := range []string{quoted + ", ", ", " + quoted, quoted + ","} {
		if strings.Contains(updated, form) {
			updated = strings.Replace(updated, form, "", 1)
			break
		}
	}
	// The only entry in the list.
	updated = strings.Replace(updated, "["+quoted+"]", "[]", 1)

	if err := backup.Save(path, original); err != nil {
		return false, err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func newSection(dir string) string {
	return fmt.Sprintf("%s\nwritable_roots = [%q]\n", sandboxSection, dir)
}
