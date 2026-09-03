// Package cmdutil holds what every command shares: the global flags that pick
// the store, and the formatting helpers more than one command needs.
package cmdutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/codyhartsook/multiplayer/internal/notify"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/httpstore"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// Environment variables that supply defaults for the global flags.
const (
	EnvDB       = "MULTIPLAYER_DB"
	EnvServer   = "MULTIPLAYER_SERVER"
	EnvDebug    = "MULTIPLAYER_DEBUG"
	EnvAutoJoin = "MULTIPLAYER_AUTO_JOIN"
)

// Options holds the global flags that decide which store the command talks to.
type Options struct {
	DB     string
	Server string
	// Human renders for a person rather than an agent: more columns, less
	// terse. Agents are the default audience.
	Human bool
}

// FromEnv seeds the global flags from the environment.
func FromEnv() *Options {
	return &Options{DB: os.Getenv(EnvDB), Server: os.Getenv(EnvServer)}
}

// OpenStore returns the store the flags select: a registry server when one is
// configured, otherwise the local SQLite database. Both satisfy store.Store, so
// no command needs to know which it got.
func (o *Options) OpenStore() (store.Store, error) {
	if o.Server != "" {
		return httpstore.New(o.Server), nil
	}
	path, err := o.DBPath()
	if err != nil {
		return nil, err
	}
	return sqlitestore.Open(path)
}

// DBPath resolves the database location, defaulting to ~/.multiplayer/sessions.db.
func (o *Options) DBPath() (string, error) {
	if o.DB != "" {
		return o.DB, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".multiplayer", "sessions.db"), nil
}

// StoreDir is the directory holding the database and hook log, which is what a
// sandboxed harness needs write access to.
func (o *Options) StoreDir() (string, error) {
	path, err := o.DBPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

// SignalBroker asks the broker to sweep now. A missed signal only costs
// latency, so failures are ignored.
func (o *Options) SignalBroker() {
	if path, err := o.DBPath(); err == nil {
		_ = notify.Signal(notify.SocketPath(path))
	}
}
