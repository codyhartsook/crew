// Package skill installs crew's room and role skills, never over a local edit.
package skill

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/version"
)

//go:embed skill.md
var doc []byte

// Doc is the bundled room skill.
func Doc() []byte { return doc }

// Name is the directory the skill is installed under in both harnesses.
const Name = "crew-rooms"

// markerFile records the hash of the skill this tool last wrote, so a re-init
// can tell its own previous output from something you edited.
const markerFile = ".installed"

// legacyName is where this tool installed the skill before it was renamed.
const legacyName = "multiplayer-rooms"

// Outcome is what happened to a skill file.
type Outcome int

const (
	Unchanged Outcome = iota
	Written
	Preserved // you edited it; a new version was left alongside
)

// Install writes content to path. A hash of the last version this tool wrote
// tells its output from a hand-edited skill, which must not be overwritten.
func Install(path string, content []byte, dryRun bool) (Outcome, error) {
	existing, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if dryRun {
			return Written, nil
		}
		return Written, write(path, content, "")
	case err != nil:
		return Unchanged, fmt.Errorf("read %s: %w", path, err)
	}

	if bytes.Equal(existing, content) {
		// Ours with no marker recorded (init predates markers). Record it,
		// or the next change would look like a local edit.
		if !dryRun && !Ours(path, existing) {
			if err := writeMarker(path, existing, ""); err != nil {
				return Unchanged, err
			}
		}
		return Unchanged, nil
	}
	if !Ours(path, existing) {
		if dryRun {
			return Preserved, nil
		}
		return Preserved, os.WriteFile(path+".new", content, 0o644)
	}
	if dryRun {
		return Written, nil
	}
	return Written, write(path, content, "")
}

// Ours reports whether content is exactly what this tool last wrote here.
func Ours(path string, content []byte) bool {
	recorded, _ := readMarker(path)
	return recorded == digest(content)
}

// readMarker returns the hash and the version that wrote it.
func readMarker(path string) (hash, version string) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), markerFile))
	if err != nil {
		return "", ""
	}
	lines := strings.Fields(string(data))
	if len(lines) == 0 {
		return "", ""
	}
	if len(lines) == 1 {
		return lines[0], "unknown"
	}
	return lines[0], lines[1]
}

// write replaces the skill, then its marker; a mismatch in between reads as an edit.
func write(path string, content []byte, sessionKey string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	if err := writeAtomic(path, content); err != nil {
		return err
	}
	return writeMarker(path, content, sessionKey)
}

// writeMarker records the hash, version and, for a role skill, its session.
func writeMarker(path string, content []byte, sessionKey string) error {
	marker := digest(content) + "\n" + version.String() + "\n"
	if sessionKey != "" {
		marker += sessionKey + "\n"
	}
	return writeAtomic(filepath.Join(filepath.Dir(path), markerFile), []byte(marker))
}

// writeAtomic renames a temp file into place so syncs never see half a file.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RemoveLegacy deletes skills installed under an earlier name, reporting
// whether anything went. One you edited is yours and is left alone.
func RemoveLegacy(skillsDir string, dryRun bool) (bool, error) {
	return removeOurs(filepath.Join(skillsDir, legacyName), dryRun)
}

// removeOurs deletes a skill dir only if its content is still ours.
func removeOurs(dir string, dryRun bool) (bool, error) {
	path := filepath.Join(dir, "SKILL.md")
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", dir, err)
	}
	if !Ours(path, content) {
		return false, nil
	}
	if !dryRun {
		if err := os.RemoveAll(dir); err != nil {
			return true, fmt.Errorf("remove %s: %w", dir, err)
		}
	}
	return true, nil
}
