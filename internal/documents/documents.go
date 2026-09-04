// Package documents stores durable room artifacts as ordinary local files.
package documents

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RoomDir returns the private local directory for a room.
func RoomDir(storeDir, roomKey string) string {
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(roomKey)))[:12]
	return filepath.Join(storeDir, "rooms", id)
}

// Dir creates and returns a room's document directory.
func Dir(storeDir, roomKey string) (string, error) {
	dir := filepath.Join(RoomDir(storeDir, roomKey), "documents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create document store: %w", err)
	}
	return dir, nil
}

// List returns document paths relative to the room's document directory.
func List(dir string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(name))
		return nil
	})
	sort.Strings(names)
	return names, err
}

// Publish copies one file into the room without overwriting an existing document.
func Publish(source, dir string) (string, error) {
	absSource, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve document: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve document store: %w", err)
	}
	if relative, err := filepath.Rel(absDir, absSource); err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		info, err := os.Stat(absSource)
		if err != nil {
			return "", fmt.Errorf("inspect document: %w", err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("document must be a regular file")
		}
		return filepath.ToSlash(relative), nil
	}

	in, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open document: %w", err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect document: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("document must be a regular file")
	}

	name := filepath.Base(source)
	target := filepath.Join(dir, name)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("document %q already exists", name)
		}
		return "", fmt.Errorf("create document: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(target)
		return "", fmt.Errorf("copy document: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("close document: %w", err)
	}
	return name, nil
}
