// Package server runs the local API, dashboard and notification broker.
// serve, init and dashboard all need it, so it sits beside them.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/documents"
	"github.com/codyhartsook/multiplayer/internal/notify"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
	"github.com/codyhartsook/multiplayer/internal/version"
)

// shutdownGrace is how long in-flight requests get to finish on shutdown.
const shutdownGrace = 5 * time.Second

// DefaultAddr is loopback by default: the registry describes local processes
// and has no authentication of its own.
const DefaultAddr = "127.0.0.1:8790"

// probeTimeout bounds the check for a registry already listening on the address.
const probeTimeout = 750 * time.Millisecond

// Config is how a caller wants the registry run.
type Config struct {
	Addr    string
	Verbose bool // log every request
	Log     io.Writer
	// OnReady receives the base URL once the listener is open.
	OnReady func(baseURL string)
}

// Serve runs the registry until ctx is cancelled or a termination signal
// arrives.
func Serve(ctx context.Context, opts *cmdutil.Options, cfg Config) error {
	// A server that proxied to another server would be a loop.
	if opts.Server != "" {
		return errors.New("this command reads a local database; unset --server")
	}
	path, err := opts.DBPath()
	if err != nil {
		return err
	}
	st, err := sqlitestore.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()
	lifecycleFile, err := os.OpenFile(filepath.Join(filepath.Dir(path), "broker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open broker log: %w", err)
	}
	defer lifecycleFile.Close()
	lifecycle := slog.New(slog.NewTextHandler(lifecycleFile, &slog.HandlerOptions{Level: slog.LevelInfo}))

	level := slog.LevelWarn
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	if cfg.Log == nil {
		cfg.Log = io.Discard
	}
	log := slog.New(slog.NewTextHandler(cfg.Log, &slog.HandlerOptions{Level: level}))

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	var background sync.WaitGroup
	start := func(run func(context.Context)) {
		background.Add(1)
		go func() {
			defer background.Done()
			run(ctx)
		}()
	}
	defer func() {
		cancel()
		background.Wait()
	}()
	roomDir := func(key string) (string, error) { return documents.Dir(filepath.Dir(path), key) }
	srv := &http.Server{
		Handler: api.New(st, log,
			api.WithUI(dashboardPage()),
			api.WithTableUI(tablePage()),
			api.WithDocuments(roomDir),
			api.WithRoomOpener(openDirectory),
		).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info("registry listening", "addr", ln.Addr().String(), "db", path)
	lifecycle.Info("broker started", "version", version.String(), "addr", ln.Addr().String())
	if cfg.OnReady != nil {
		cfg.OnReady("http://" + ln.Addr().String())
	}

	broker := notify.NewBroker(st, log, notify.DefaultInterval)
	if err := notify.ListenSignals(ctx, notify.SocketPath(path), broker.Trigger); err != nil {
		log.Warn("notification signals unavailable; using polling", "err", err)
	}
	start(broker.Run)
	start(newUsageCoordinator(st, log).run)
	liveness := newLivenessCoordinator(st, log)
	liveness.skillsHome, _ = os.UserHomeDir()
	start(liveness.run)
	err = run(ctx, srv, ln, lifecycle)
	if err == nil {
		lifecycle.Info("broker stopped")
	}
	return err
}

func openDirectory(path string) error {
	cmd := exec.Command("code", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// IsUp reports whether a healthy registry already answers at baseURL.
func IsUp(ctx context.Context, baseURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// run serves until the context is cancelled or a termination signal arrives,
// then drains in-flight requests.
func run(ctx context.Context, srv *http.Server, ln net.Listener, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errc <- err
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("broker shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errc
	}
}
