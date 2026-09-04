package documents

import (
	"os"
	"path/filepath"
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
