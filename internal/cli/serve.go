package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/api"
	"github.com/codyhartsook/multiplayer/internal/ui"
)

// shutdownGrace is how long in-flight requests get to finish on shutdown.
const shutdownGrace = 5 * time.Second

// defaultAddr is loopback by default: the registry describes local processes
// and has no authentication of its own.
const defaultAddr = "127.0.0.1:8790"

func newServeCmd(opts *options) *cobra.Command {
	var (
		addr    string
		verbose bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the registry API and dashboard over HTTP",
		Long: `Reads and writes the same local database the CLI uses. Setting
MULTIPLAYER_SERVER points hooks at it, so they write through the API instead of
opening the database directly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serveRegistry(cmd, opts, addr, verbose, func(baseURL string) {
				fmt.Fprintf(cmd.OutOrStdout(), "dashboard  %s\n", baseURL)
				fmt.Fprintf(cmd.OutOrStdout(), "hooks      export MULTIPLAYER_SERVER=%s\n", baseURL)
			})
		},
	}

	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to listen on")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log every request")
	return cmd
}

// serveRegistry runs the registry until the command's context is cancelled or a
// termination signal arrives. onReady receives the resolved base URL once the
// listener is open, which is where callers announce themselves or open a browser.
func serveRegistry(cmd *cobra.Command, opts *options, addr string, verbose bool, onReady func(baseURL string)) error {
	// A server that proxied to another server would be a loop.
	if opts.server != "" {
		return errors.New("this command reads a local database; unset --server")
	}
	st, err := opts.openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level}))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           api.New(st, log, api.WithUI(ui.Handler())).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	db, _ := opts.dbPath()
	log.Info("registry listening", "addr", ln.Addr().String(), "db", db)
	if onReady != nil {
		onReady("http://" + ln.Addr().String())
	}
	return run(cmd.Context(), srv, ln, log)
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
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errc
	}
}
