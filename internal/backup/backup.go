// Package backup preserves a file's previous contents before it is rewritten.
package backup

import (
	"fmt"
	"os"
	"time"
)

// Save copies original aside next to path. Writing nothing is not an edit, so
// empty contents produce no file.
func Save(path string, original []byte) error {
	if len(original) == 0 {
		return nil
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dst := fmt.Sprintf("%s.bak-%d", path, time.Now().UTC().UnixNano())
	if err := os.WriteFile(dst, original, mode); err != nil {
		return fmt.Errorf("back up %s: %w", path, err)
	}
	if err := os.Chmod(dst, mode); err != nil {
		return fmt.Errorf("set backup permissions for %s: %w", path, err)
	}
	return nil
}
