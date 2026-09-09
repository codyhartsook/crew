package registry

import (
	"context"
	"log/slog"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

type fakeUsageStore struct {
	sessions []*session.Session
	writes   map[string]usage.Snapshot
	count    int
}

func (s *fakeUsageStore) List(_ context.Context, _ store.Filter) ([]*session.Session, error) {
	return s.sessions, nil
}

func (s *fakeUsageStore) SetUsage(_ context.Context, key string, snapshot *usage.Snapshot) error {
	if s.writes == nil {
		s.writes = map[string]usage.Snapshot{}
	}
	s.writes[key] = *snapshot
	s.count++
	return nil
}

type fakeSource struct{ sampler usage.Sampler }

func (s fakeSource) Open(_, _ string) usage.Sampler { return s.sampler }

type fakeSampler struct {
	snapshot usage.Snapshot
	changed  bool
}

func (s *fakeSampler) Refresh(context.Context) (usage.Snapshot, bool, error) {
	changed := s.changed
	s.changed = false
	return s.snapshot, changed, nil
}

func TestUsageCoordinatorSamplesAnyRegisteredHarnessLocally(t *testing.T) {
	local := &session.Session{
		ID: "one", Harness: "foo", Status: session.StatusActive, Host: "here",
	}
	remote := &session.Session{
		ID: "two", Harness: "foo", Status: session.StatusActive, Host: "elsewhere",
	}
	st := &fakeUsageStore{sessions: []*session.Session{local, remote}}
	sam := &fakeSampler{snapshot: usage.Snapshot{ContextWindow: 100, ContextUsed: 80}, changed: true}
	c := newUsageCoordinator(st, slog.New(slog.DiscardHandler))
	c.host = "here"
	c.sources = func(h session.Harness) usage.Source {
		if h == "foo" {
			return fakeSource{sampler: sam}
		}
		return nil
	}

	c.sweep(context.Background())
	if got, ok := st.writes[local.Key()]; !ok || got.ContextUsed != 80 {
		t.Errorf("local usage = %+v, want sampled snapshot", got)
	}
	if _, ok := st.writes[remote.Key()]; ok {
		t.Error("remote session was sampled from this host")
	}
	c.sweep(context.Background())
	if st.count != 1 {
		t.Errorf("writes = %d, want unchanged sampler not rewritten", st.count)
	}
}
