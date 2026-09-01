// Package storetest is a conformance suite every store.Store implementation
// must pass, so a new backend satisfies one interface and runs one test rather
// than rediscovering the semantics the hook relies on.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// Factory builds an empty store for one subtest. Cleanup is the caller's job,
// normally via t.Cleanup inside the factory.
type Factory func(t *testing.T) store.Store

// Run executes the whole suite against the implementation newStore builds.
func Run(t *testing.T, newStore Factory) {
	t.Helper()
	tests := map[string]func(*testing.T, Factory){
		"RoundTrip":            testRoundTrip,
		"UpsertPreservesStart": testUpsertPreservesStart,
		"GetUnknown":           testGetUnknown,
		"End":                  testEnd,
		"EndIsIdempotent":      testEndIsIdempotent,
		"EndUnknown":           testEndUnknown,
		"ListFilters":          testListFilters,
		"ListOrdering":         testListOrdering,
		"Delete":               testDelete,
		"ConcurrentUpsert":     testConcurrentUpsert,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { fn(t, newStore) })
	}
}

// base is a fully populated session: a codex agent holding a leased pool slot.
func base() *session.Session {
	start := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	return &session.Session{
		ID:      "sess-1",
		Harness: session.HarnessCodex,
		Status:  session.StatusActive,
		PID:     4242,
		Host:    "laptop",
		User:    "cody",
		CWD:     "/pool/kagent-9f7087/2/kagent",
		Repo: &session.Repo{
			Name:       "kagent",
			Root:       "/pool/kagent-9f7087/2/kagent",
			MainRoot:   "/src/kagent",
			Remote:     "ssh://git@github.com/kagent-dev/kagent.git",
			Head:       "abc123",
			Detached:   true,
			IsWorktree: true,
		},
		Pool: &session.Pool{
			Manager:     "treehouse",
			Name:        "kagent-9f7087",
			Slot:        "2",
			Root:        "/pool/kagent-9f7087/2/kagent",
			Leased:      true,
			LeaseID:     "70b6d0fd",
			LeaseHolder: "agent:agenttemplate-create-apply",
		},
		StartedAt: start,
		LastSeen:  start,
		Meta:      map[string]string{"model": "gpt-5.6-terra", "source": "startup"},
	}
}

func testRoundTrip(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	want := base()

	if err := s.Upsert(ctx, want); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.Get(ctx, want.Key())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	assertSameSession(t, want, got)
}

func testUpsertPreservesStart(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	first := base()
	if err := s.Upsert(ctx, first); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}

	// A resumed session reports a new start time; the store must keep the
	// original one while taking every other field from the new record.
	second := base()
	second.StartedAt = first.StartedAt.Add(time.Hour)
	second.LastSeen = first.LastSeen.Add(time.Hour)
	second.Repo.Branch = "feature/x"
	second.Meta = map[string]string{"source": "resume"}
	if err := s.Upsert(ctx, second); err != nil {
		t.Fatalf("Upsert second: %v", err)
	}

	got, err := s.Get(ctx, second.Key())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.StartedAt.Equal(first.StartedAt) {
		t.Errorf("StartedAt = %v, want the original %v", got.StartedAt, first.StartedAt)
	}
	if !got.LastSeen.Equal(second.LastSeen) {
		t.Errorf("LastSeen = %v, want %v", got.LastSeen, second.LastSeen)
	}
	if got.Repo.Branch != "feature/x" {
		t.Errorf("Repo.Branch = %q, want %q", got.Repo.Branch, "feature/x")
	}
	if got.Meta["source"] != "resume" {
		t.Errorf("Meta[source] = %q, want %q", got.Meta["source"], "resume")
	}
	if _, ok := got.Meta["model"]; ok {
		t.Error("Meta should be replaced wholesale, not merged")
	}
}

func testGetUnknown(t *testing.T, newStore Factory) {
	s := newStore(t)
	if _, err := s.Get(context.Background(), "codex:nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get unknown: err = %v, want store.ErrNotFound", err)
	}
}

// Touch is what keeps "last seen" meaning last active rather than started.
func testTouch(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	sess := base()
	if err := s.Upsert(ctx, sess); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	later := sess.StartedAt.Add(2 * time.Hour)
	if err := s.Touch(ctx, sess.Key(), later); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, err := s.Get(ctx, sess.Key())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.LastSeen.Equal(later) {
		t.Errorf("LastSeen = %v, want %v", got.LastSeen, later)
	}
	if !got.StartedAt.Equal(sess.StartedAt) {
		t.Errorf("StartedAt = %v, want it unchanged at %v", got.StartedAt, sess.StartedAt)
	}
	if got.Status != session.StatusActive {
		t.Errorf("Status = %q, want it left active", got.Status)
	}

	if err := s.Touch(ctx, "codex:nope", later); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Touch on an unknown key: err = %v, want store.ErrNotFound", err)
	}

	if err := s.End(ctx, sess.Key(), later, "exit"); err != nil {
		t.Fatalf("End: %v", err)
	}
	if err := s.Touch(ctx, sess.Key(), later.Add(time.Hour)); err != nil {
		t.Fatalf("Touch after End: %v", err)
	}
	got, err = s.Get(ctx, sess.Key())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != session.StatusEnded {
		t.Errorf("Status = %q, want it to stay ended", got.Status)
	}
}

func testEnd(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	sess := base()
	if err := s.Upsert(ctx, sess); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	endedAt := sess.StartedAt.Add(30 * time.Minute)
	if err := s.End(ctx, sess.Key(), endedAt, "exit"); err != nil {
		t.Fatalf("End: %v", err)
	}

	got, err := s.Get(ctx, sess.Key())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != session.StatusEnded {
		t.Errorf("Status = %q, want %q", got.Status, session.StatusEnded)
	}
	if got.Active() {
		t.Error("Active() = true after End")
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(endedAt) {
		t.Errorf("EndedAt = %v, want %v", got.EndedAt, endedAt)
	}
	if got.EndReason != "exit" {
		t.Errorf("EndReason = %q, want %q", got.EndReason, "exit")
	}
}

func testEndIsIdempotent(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	sess := base()
	if err := s.Upsert(ctx, sess); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	first := sess.StartedAt.Add(time.Minute)
	if err := s.End(ctx, sess.Key(), first, "exit"); err != nil {
		t.Fatalf("End first: %v", err)
	}
	if err := s.End(ctx, sess.Key(), first.Add(time.Hour), "other"); err != nil {
		t.Fatalf("End second: %v", err)
	}

	got, err := s.Get(ctx, sess.Key())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(first) {
		t.Errorf("EndedAt = %v, want the first end %v", got.EndedAt, first)
	}
	if got.EndReason != "exit" {
		t.Errorf("EndReason = %q, want the first reason %q", got.EndReason, "exit")
	}
}

func testEndUnknown(t *testing.T, newStore Factory) {
	s := newStore(t)
	err := s.End(context.Background(), "codex:nope", time.Now(), "exit")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("End unknown: err = %v, want store.ErrNotFound", err)
	}
}

func testListFilters(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	// A pooled codex session, a plain claude session in the same repo's main
	// checkout, and an ended claude session in an unrelated repo.
	pooled := base()
	pooled.LastSeen = now

	plain := base()
	plain.ID = "sess-2"
	plain.Harness = session.HarnessClaude
	plain.Pool = nil
	plain.Repo = &session.Repo{Name: "kagent", Root: "/src/kagent", MainRoot: "/src/kagent", Branch: "main"}
	plain.CWD = "/src/kagent"
	plain.LastSeen = now.Add(-time.Minute)

	ended := base()
	ended.ID = "sess-3"
	ended.Harness = session.HarnessClaude
	ended.Status = session.StatusEnded
	ended.Pool = nil
	ended.Repo = &session.Repo{Name: "home-base", Root: "/src/home-base", MainRoot: "/src/home-base", Branch: "main"}
	ended.CWD = "/src/home-base"
	ended.LastSeen = now.Add(-2 * time.Minute)

	for _, sess := range []*session.Session{pooled, plain, ended} {
		if err := s.Upsert(ctx, sess); err != nil {
			t.Fatalf("Upsert %s: %v", sess.Key(), err)
		}
	}

	cases := []struct {
		name   string
		filter store.Filter
		want   []string
	}{
		{"all", store.Filter{}, []string{pooled.Key(), plain.Key(), ended.Key()}},
		{"harness", store.Filter{Harness: session.HarnessClaude}, []string{plain.Key(), ended.Key()}},
		{"status", store.Filter{Status: session.StatusActive}, []string{pooled.Key(), plain.Key()}},
		{"repo name", store.Filter{RepoName: "kagent"}, []string{pooled.Key(), plain.Key()}},
		{"repo root", store.Filter{RepoRoot: "/src/kagent"}, []string{plain.Key()}},
		{"pooled only", store.Filter{PooledOnly: true}, []string{pooled.Key()}},
		{"combined", store.Filter{Harness: session.HarnessClaude, Status: session.StatusActive}, []string{plain.Key()}},
		{"limit", store.Filter{Limit: 2}, []string{pooled.Key(), plain.Key()}},
		{"no match", store.Filter{RepoName: "absent"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.List(ctx, tc.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			assertKeys(t, got, tc.want)
		})
	}
}

func testListOrdering(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	var want []string
	// Insert oldest first so a store that simply returns insertion order fails.
	for i := 0; i < 4; i++ {
		sess := base()
		sess.ID = fmt.Sprintf("sess-%d", i)
		sess.LastSeen = now.Add(time.Duration(i) * time.Minute)
		if err := s.Upsert(ctx, sess); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		want = append([]string{sess.Key()}, want...)
	}

	got, err := s.List(ctx, store.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	assertKeys(t, got, want)
}

func testDelete(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)
	sess := base()
	if err := s.Upsert(ctx, sess); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Delete(ctx, sess.Key()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, sess.Key()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after Delete: err = %v, want store.ErrNotFound", err)
	}
	if err := s.Delete(ctx, sess.Key()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete unknown: err = %v, want store.ErrNotFound", err)
	}
}

// testConcurrentUpsert models the real workload: several harnesses firing
// session hooks at the same instant.
func testConcurrentUpsert(t *testing.T, newStore Factory) {
	ctx := context.Background()
	s := newStore(t)

	const n = 16
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess := base()
			sess.ID = fmt.Sprintf("sess-%d", i)
			errs <- s.Upsert(ctx, sess)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Upsert: %v", err)
		}
	}

	got, err := s.List(ctx, store.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != n {
		t.Fatalf("List returned %d sessions, want %d", len(got), n)
	}
}

func assertKeys(t *testing.T, got []*session.Session, want []string) {
	t.Helper()
	var keys []string
	for _, s := range got {
		keys = append(keys, s.Key())
	}
	if len(keys) != len(want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys = %v, want %v", keys, want)
		}
	}
}

func assertSameSession(t *testing.T, want, got *session.Session) {
	t.Helper()
	if got.ID != want.ID || got.Harness != want.Harness || got.Status != want.Status {
		t.Errorf("identity = (%s, %s, %s), want (%s, %s, %s)", got.ID, got.Harness, got.Status, want.ID, want.Harness, want.Status)
	}
	if got.PID != want.PID || got.Host != want.Host || got.User != want.User || got.CWD != want.CWD {
		t.Errorf("process fields = (%d, %s, %s, %s), want (%d, %s, %s, %s)",
			got.PID, got.Host, got.User, got.CWD, want.PID, want.Host, want.User, want.CWD)
	}
	if got.Repo == nil || *got.Repo != *want.Repo {
		t.Errorf("Repo = %+v, want %+v", got.Repo, want.Repo)
	}
	if got.Pool == nil || *got.Pool != *want.Pool {
		t.Errorf("Pool = %+v, want %+v", got.Pool, want.Pool)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, want.StartedAt)
	}
	if !got.LastSeen.Equal(want.LastSeen) {
		t.Errorf("LastSeen = %v, want %v", got.LastSeen, want.LastSeen)
	}
	if len(got.Meta) != len(want.Meta) {
		t.Fatalf("Meta = %v, want %v", got.Meta, want.Meta)
	}
	for k, v := range want.Meta {
		if got.Meta[k] != v {
			t.Errorf("Meta[%q] = %q, want %q", k, got.Meta[k], v)
		}
	}
}
