package notify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SocketPath keeps the event socket beside the database shared by writers and
// the broker.
func SocketPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "notify.sock")
}

// ListenSignals turns local datagrams into immediate broker sweeps.
func ListenSignals(ctx context.Context, path string, trigger func()) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("notify signal path is not a socket: %s", path)
		}
		if Signal(path) == nil {
			return fmt.Errorf("notify signal socket already in use: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale notify socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	addr := &net.UnixAddr{Name: path, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		return fmt.Errorf("listen for notify signals: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		conn.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		_ = os.Remove(path)
		conn.Close()
	}()
	go func() {
		var buf [1]byte
		for {
			if _, _, err := conn.ReadFromUnix(buf[:]); err != nil {
				return
			}
			trigger()
		}
	}()
	return nil
}

// Signal asks a running broker to sweep now. Callers keep polling as the
// reliability path, so a missing socket is harmless.
func Signal(path string) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	_, err = conn.Write([]byte{1})
	return err
}
