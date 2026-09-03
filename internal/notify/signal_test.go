package notify

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSignalTriggersListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan struct{}, 1)
	path := filepath.Join(t.TempDir(), "notify.sock")
	if err := ListenSignals(ctx, path, func() { got <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	if err := Signal(path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("signal was not delivered")
	}
}
