package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/documents"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

const roomKey = "/repo"

// docServer returns a handler with a document store rooted at a temp dir, and
// the room the tests publish into.
func docServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	membership := &room.Membership{SessionKey: "codex:one", Room: roomKey, Scope: room.ScopeWorktree, JoinedAt: time.Now()}
	if err := store.Join(t.Context(), membership); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	handler := api.New(store, nil, api.WithDocuments(func(key string) (string, error) {
		return documents.Dir(root, key)
	})).Handler()
	dir, err := documents.Dir(root, roomKey)
	if err != nil {
		t.Fatal(err)
	}
	return handler, dir
}

// call issues a loopback, same-origin request, which is the only kind the
// document endpoints answer.
func call(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, h, httptest.NewRequest(method, target, nil))
}

// send stamps a prepared request as local and same-origin, then serves it.
func send(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Origin", "http://localhost")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func upload(t *testing.T, name, body string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost,
		"http://localhost/v1/rooms/documents?room="+url.QueryEscape(roomKey), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func listDocs(t *testing.T, h http.Handler) []documents.Document {
	t.Helper()
	rec := call(t, h, http.MethodGet,
		"http://localhost/v1/rooms/documents?room="+url.QueryEscape(roomKey))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got api.DocumentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Documents
}

func TestDocumentRoundTrip(t *testing.T) {
	h, dir := docServer(t)

	if docs := listDocs(t, h); len(docs) != 0 {
		t.Fatalf("new room has documents: %v", docs)
	}

	if rec := send(t, h, upload(t, "plan.md", "the plan")); rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", rec.Code, rec.Body)
	}
	body, err := os.ReadFile(filepath.Join(dir, "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "the plan" {
		t.Errorf("stored document = %q", body)
	}

	docs := listDocs(t, h)
	if len(docs) != 1 || docs[0].Name != "plan.md" || docs[0].Size != 8 {
		t.Fatalf("documents = %+v, want one plan.md of 8 bytes", docs)
	}

	rec := call(t, h, http.MethodDelete,
		"http://localhost/v1/rooms/documents?room="+url.QueryEscape(roomKey)+"&name=plan.md")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if docs := listDocs(t, h); len(docs) != 0 {
		t.Fatalf("documents after delete = %v", docs)
	}
}

// The timeline is the room's record, so a document change has to show up there
// the same way the CLI's does.
func TestDocumentChangesAreAnnounced(t *testing.T) {
	h, _ := docServer(t)
	if rec := send(t, h, upload(t, "plan.md", "x")); rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d: %s", rec.Code, rec.Body)
	}
	call(t, h, http.MethodDelete,
		"http://localhost/v1/rooms/documents?room="+url.QueryEscape(roomKey)+"&name=plan.md")

	rec := call(t, h, http.MethodGet, "http://localhost/v1/entries?room="+url.QueryEscape(roomKey))
	var got api.EntriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(got.Entries))
	}
	for i, want := range []string{documents.PublishedNote + "plan.md", documents.RemovedNote + "plan.md"} {
		if got.Entries[i].Body != want {
			t.Errorf("entry %d = %q, want %q", i, got.Entries[i].Body, want)
		}
		if got.Entries[i].Scope != room.ScopeWorktree {
			t.Errorf("entry %d scope = %q, want the room's own", i, got.Entries[i].Scope)
		}
		if !strings.HasPrefix(got.Entries[i].Author, "human:") {
			t.Errorf("entry %d author = %q, want a human", i, got.Entries[i].Author)
		}
	}
}

func TestUploadDoesNotOverwrite(t *testing.T) {
	h, _ := docServer(t)
	if rec := send(t, h, upload(t, "plan.md", "first")); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	rec := send(t, h, upload(t, "plan.md", "second"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate upload = %d, want 400", rec.Code)
	}
	if docs := listDocs(t, h); len(docs) != 1 {
		t.Fatalf("documents = %v, want one", docs)
	}
}

// A filename is browser data. mime/multipart strips its directory part (RFC 7578),
// so an upload lands as a plain name in the room, never above it.
func TestUploadFilenameCannotEscapeTheStore(t *testing.T) {
	h, dir := docServer(t)
	for _, name := range []string{"../escape.md", "sub/nested.md"} {
		if rec := send(t, h, upload(t, name, "x")); rec.Code != http.StatusOK {
			t.Errorf("upload of %q = %d, want 200: %s", name, rec.Code, rec.Body)
		}
	}
	for _, gone := range []string{"escape.md", "sub"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), gone)); err == nil {
			t.Errorf("upload created %s outside the document store", gone)
		}
	}
	if got := documents.Names(listDocs(t, h)); len(got) != 2 || got[0] != "escape.md" || got[1] != "nested.md" {
		t.Errorf("documents = %v, want the base names", got)
	}
}

// A name that is not a filename at all is rejected, not repaired.
func TestUploadRejectsUnusableFilename(t *testing.T) {
	h, _ := docServer(t)
	for _, name := range []string{"", "   ", ".DS_Store", ".."} {
		if rec := send(t, h, upload(t, name, "x")); rec.Code != http.StatusBadRequest {
			t.Errorf("upload of %q = %d, want 400", name, rec.Code)
		}
	}
	if docs := listDocs(t, h); len(docs) != 0 {
		t.Errorf("documents = %v, want none", documents.Names(docs))
	}
}

// Delete takes its name from the query string, where nothing normalises it.
func TestDeleteRejectsPathInName(t *testing.T) {
	h, dir := docServer(t)
	secret := filepath.Join(filepath.Dir(dir), "ROOM.md")
	if err := os.WriteFile(secret, []byte("snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../ROOM.md", "..%2FROOM.md", "sub/plan.md"} {
		rec := call(t, h, http.MethodDelete, "http://localhost/v1/rooms/documents?room="+
			url.QueryEscape(roomKey)+"&name="+url.QueryEscape(name))
		if rec.Code == http.StatusNoContent {
			t.Errorf("delete accepted %q", name)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("delete reached outside the document store: %v", err)
	}
}

func TestDeleteReportsMissingDocument(t *testing.T) {
	h, _ := docServer(t)
	rec := call(t, h, http.MethodDelete,
		"http://localhost/v1/rooms/documents?room="+url.QueryEscape(roomKey)+"&name=nothing.md")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDocumentEndpointsAreLocalOnly(t *testing.T) {
	h, _ := docServer(t)
	target := "http://localhost/v1/rooms/documents?room=" + url.QueryEscape(roomKey)

	remote := httptest.NewRequest(http.MethodGet, target, nil)
	remote.RemoteAddr = "10.0.0.4:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, remote)
	if rec.Code != http.StatusForbidden {
		t.Errorf("off-host list = %d, want 403", rec.Code)
	}

	crossOrigin := upload(t, "plan.md", "x")
	crossOrigin.RemoteAddr = "127.0.0.1:1234"
	crossOrigin.Header.Set("Origin", "https://example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, crossOrigin)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin upload = %d, want 403", rec.Code)
	}
}

func TestDocumentEndpointsRejectUnknownRoom(t *testing.T) {
	h, _ := docServer(t)
	rec := call(t, h, http.MethodGet, "http://localhost/v1/rooms/documents?room=%2Fmissing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown room = %d, want 404", rec.Code)
	}
	rec = call(t, h, http.MethodGet, "http://localhost/v1/rooms/documents")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing room = %d, want 400", rec.Code)
	}
}

// Without the option there is no document store to serve, which is what a
// remote registry should look like.
func TestDocumentEndpointsAbsentWithoutStore(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := api.New(store, nil).Handler()
	rec := call(t, h, http.MethodGet, "http://localhost/v1/rooms/documents?room=%2Frepo")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
