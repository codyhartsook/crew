package documents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	dir, err := Dir(root, "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(source, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(source, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(source, dir); err == nil {
		t.Fatal("Publish overwrote an existing document")
	}
	got, err := os.ReadFile(filepath.Join(dir, "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("published document = %q, want first version", got)
	}
}

func TestPublishAcceptsDocumentAlreadyInStore(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(path, []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, err := Publish(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if name != "plan.md" {
		t.Fatalf("published name = %q", name)
	}
}

// The store directory is opened in a file browser, so it collects files nobody
// published. Only what somebody published is a document.
func TestListSkipsHiddenFiles(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plan.md", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".trash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".trash", "gone.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	docs, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := Names(docs); len(got) != 1 || got[0] != "plan.md" {
		t.Fatalf("documents = %v, want [plan.md]", got)
	}
}

func TestListReportsSize(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("documents = %v, want one", docs)
	}
	if docs[0].Size != 5 {
		t.Errorf("size = %d, want 5", docs[0].Size)
	}
	if docs[0].ModTime.IsZero() {
		t.Error("mod time is zero")
	}
}

// Removing is recoverable on purpose: the room stops showing the document, and
// the file is still on disk for whoever wishes they had not.
func TestRemoveTrashesDocument(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}

	trashed, err := Remove(dir, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plan.md")); !os.IsNotExist(err) {
		t.Error("document is still published")
	}
	body, err := os.ReadFile(trashed)
	if err != nil {
		t.Fatalf("trashed document is not readable: %v", err)
	}
	if string(body) != "plan" {
		t.Errorf("trashed document = %q, want plan", body)
	}
	docs, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Errorf("documents = %v, want none", Names(docs))
	}
}

// A removed document frees its name, and removing the replacement must not
// lose the original.
func TestRemoveKeepsBothVersionsOfAName(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Remove(dir, "plan.md"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".trash"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("trash holds %d files, want 2", len(entries))
	}
}

func TestRemoveReportsMissingDocument(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(dir, "nothing.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// A name is one file in the room. Anything carrying a path is rejected rather
// than reinterpreted, so a client cannot reach outside the store.
func TestDocumentNamesAreRejectedNotReinterpreted(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "  ", "../escape.md", "sub/plan.md", "/etc/passwd", ".hidden", ".."} {
		t.Run("add "+name, func(t *testing.T) {
			if _, err := Add(dir, name, strings.NewReader("x")); err == nil {
				t.Fatalf("Add accepted %q", name)
			}
		})
		t.Run("remove "+name, func(t *testing.T) {
			if _, err := Remove(dir, name); err == nil {
				t.Fatalf("Remove accepted %q", name)
			}
		})
	}
	// Nothing was created anywhere under the store while trying.
	docs, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Errorf("documents = %v, want none", Names(docs))
	}
}

func TestAddDoesNotOverwrite(t *testing.T) {
	dir, err := Dir(t.TempDir(), "/src/widget")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Add(dir, "plan.md", strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(dir, "plan.md", strings.NewReader("second")); err == nil {
		t.Fatal("Add overwrote an existing document")
	}
	body, err := os.ReadFile(filepath.Join(dir, "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "first" {
		t.Errorf("document = %q, want first", body)
	}
}
