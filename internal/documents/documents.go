// Package documents stores durable room artifacts as ordinary local files.
package documents

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// trash holds removed documents. Dotted, so a listing skips it.
const trash = ".trash"

// Publishing and removing announce themselves in the room timeline. The
// prefixes are the contract the dashboard reads to render an artifact event
// rather than another wall of note text.
const (
	PublishedNote = "Published document: "
	RemovedNote   = "Removed document: "
)

// ErrNotFound reports a document that is not in the room.
var ErrNotFound = errors.New("document not found")

// Document is one published artifact.
type Document struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

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

// List returns the room's documents by name. Hidden files are not documents:
// the directory gets opened in a file browser, so it collects .DS_Store.
func List(dir string) ([]Document, error) {
	var docs []Document
	err := filepath.WalkDir(dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if hidden(name) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		docs = append(docs, Document{
			Name:    filepath.ToSlash(name),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
		return nil
	})
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	return docs, err
}

// Names returns just the document names.
func Names(docs []Document) []string {
	names := make([]string, 0, len(docs))
	for _, doc := range docs {
		names = append(names, doc.Name)
	}
	return names
}

// hidden reports whether any segment of a relative path is dotted.
func hidden(name string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(name), "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

// Add writes one document without overwriting an existing one. Every
// publisher goes through it, so an upload and a CLI path share the rules.
func Add(dir, name string, r io.Reader) (string, error) {
	clean, err := documentName(name)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, clean)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("document %q already exists", clean)
		}
		return "", fmt.Errorf("create document: %w", err)
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		_ = os.Remove(target)
		return "", fmt.Errorf("copy document: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("close document: %w", err)
	}
	return clean, nil
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
	return Add(dir, filepath.Base(source), in)
}

// Remove takes a document out of the room, moving it to the store's trash
// rather than unlinking it, so a mistake stays recoverable. Nothing empties
// the trash automatically.
func Remove(dir, name string) (string, error) {
	clean, err := documentName(name)
	if err != nil {
		return "", err
	}
	source := filepath.Join(dir, clean)
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, clean)
	}
	if err != nil {
		return "", fmt.Errorf("inspect document: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrNotFound, clean)
	}

	trashDir := filepath.Join(dir, trash)
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return "", fmt.Errorf("create document trash: %w", err)
	}
	target, err := reserve(trashDir, fmt.Sprintf("%s.%d", clean, time.Now().UTC().Unix()))
	if err != nil {
		return "", err
	}
	// Renaming over the reserved placeholder is atomic, so a colliding
	// timestamp never costs the earlier file.
	if err := os.Rename(source, target); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("remove document: %w", err)
	}
	return target, nil
}

// reserve claims an unused path in dir, preferring name.
func reserve(dir, name string) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		candidate := filepath.Join(dir, name)
		if attempt > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s.%d", name, attempt))
		}
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("reserve document trash: %w", err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("reserve document trash: %w", err)
		}
		return candidate, nil
	}
	return "", errors.New("document trash has too many copies of that name")
}

// documentName validates a client-supplied name. A document is one file in
// the room, so anything carrying a path is rejected, not reinterpreted.
func documentName(name string) (string, error) {
	clean := strings.TrimSpace(filepath.ToSlash(name))
	if clean == "" {
		return "", errors.New("document name is required")
	}
	if clean != lastSegment(clean) {
		return "", fmt.Errorf("document name %q must not contain a path", name)
	}
	if hidden(clean) {
		return "", fmt.Errorf("document name %q must not be hidden", name)
	}
	return clean, nil
}

// lastSegment returns the final element of a slash-separated name.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
