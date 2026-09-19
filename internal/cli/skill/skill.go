// Package skill installs the bundled room skill, and never over an edit you
// made. init writes it; uninstall removes it only when it is still ours.
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

// Install writes the skill to path.
//
// Overwriting a hand-edited skill is the obvious way to get this wrong: the
// skill is meant to be tuned once you see what agents actually write. A hash of
// the last version this tool wrote distinguishes its own output from yours.
func Install(path string, dryRun bool) (Outcome, error) {
	existing, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if dryRun {
			return Written, nil
		}
		return Written, write(path)
	case err != nil:
		return Unchanged, fmt.Errorf("read %s: %w", path, err)
	}

	if bytes.Equal(existing, doc) {
		// Content is ours even if no marker was recorded - an init from
		// before the marker existed. Record it, or the next change would be
		// mistaken for a local edit.
		if !dryRun && !Ours(path, existing) {
			if err := writeMarker(path, existing); err != nil {
				return Unchanged, err
			}
		}
		return Unchanged, nil
	}
	if !Ours(path, existing) {
		if dryRun {
			return Preserved, nil
		}
		return Preserved, os.WriteFile(path+".new", doc, 0o644)
	}
	if dryRun {
		return Written, nil
	}
	return Written, write(path)
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

func write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	if err := os.WriteFile(path, doc, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return writeMarker(path, doc)
}

// writeMarker records the hash and the version that wrote it, so a later init
// can say what it is replacing without guessing.
func writeMarker(path string, content []byte) error {
	marker := filepath.Join(filepath.Dir(path), markerFile)
	return os.WriteFile(marker, []byte(digest(content)+"\n"+version.String()+"\n"), 0o644)
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RemoveLegacy deletes skills installed under an earlier name, reporting
// whether anything went. One you edited is yours and is left alone.
func RemoveLegacy(skillsDir string, dryRun bool) (bool, error) {
	dir := filepath.Join(skillsDir, legacyName)
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
