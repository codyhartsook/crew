// Package claude holds what is specific to Claude Code: the channel that pushes
// room entries into a running session, and the socket a pusher writes to.
// Claude Code spawns the channel over stdio.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/codyhartsook/multiplayer/internal/proc"
)

// Name is how a session opts this channel in, and the source attribute Claude
// sees on the events.
const Name = "multiplayer"

// socketDir holds the per-session channel sockets, keyed by the session's pid.
const socketDir = "/tmp/multiplayer-channels"

// pushTimeout bounds a local socket write before a caller falls back.
const pushTimeout = 2 * time.Second

// ChannelSocket is where a session's channel listens, if it enabled one.
func ChannelSocket(pid int) string {
	return filepath.Join(socketDir, fmt.Sprintf("%d.sock", pid))
}

// Push delivers text to a session's channel. The socket exists only for a
// session that opted in, so a dial failure means there is no channel to use.
func Push(pid int, text string) error {
	return push(ChannelSocket(pid), text)
}

func push(sock, text string) error {
	conn, err := net.DialTimeout("unix", sock, pushTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(pushTimeout)); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"content": text})
	if err != nil {
		return err
	}
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	reply, err := io.ReadAll(io.LimitReader(conn, 128))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(reply), "ok") {
		return fmt.Errorf("channel refused: %s", strings.TrimSpace(string(reply)))
	}
	return nil
}

// LaunchCommand is how a session must be started for channel delivery. The
// flag has no settings equivalent, so init has to tell the user.
func LaunchCommand() string {
	return "claude " + devChannelsFlag + " server:" + Name
}

// Subcommand is what a harness runs to start this channel.
const Subcommand = "channel"

// Entry is the MCP server registration that makes a harness spawn the channel.
func Entry(exe string) map[string]any {
	return map[string]any{"command": exe, "args": []any{Subcommand}}
}

// IsOurs reports whether a registration is one we wrote, judged by the
// subcommand rather than the binary path, which moves between installs.
func IsOurs(entry any) bool {
	e, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	args, ok := e["args"].([]any)
	if !ok || len(args) != 1 {
		return false
	}
	arg, _ := args[0].(string)
	return arg == Subcommand
}

const (
	// capability marks this server as a channel; presence registers the
	// listener and the value is always empty.
	capability = "claude/channel"
	// notifyMethod is the event Claude Code delivers into the session.
	notifyMethod = "notifications/claude/channel"
	// rejectedProtocol is a revision Claude Code will not register a channel on.
	rejectedProtocol = "2026-07-28"

	channelsFlag = "--channels"
	// devChannelsFlag loads a channel that is not on Claude Code's approved
	// list, which every custom channel needs during the research preview.
	devChannelsFlag = "--dangerously-load-development-channels"
	maxPayload      = 64 * 1024
)

const instructions = "Entries addressed to this session arrive as " +
	"<channel source=\"" + Name + "\">. Read them with: multiplayer room --inbox --ack. " +
	"One-way: there is no reply tool."

// Serve runs the MCP server until ctx ends or stdin closes. It listens for
// pushes only when the owning session opted this channel in.
func Serve(ctx context.Context, version string, in io.Reader, out io.Writer) error {
	srv := newChannelServer(version, in, out)
	pid := enabledFor()
	if pid == 0 {
		return srv.Serve(ctx)
	}
	ln, err := listen(pid)
	if err != nil {
		return err
	}
	defer ln.Close()

	// Closing the listener unlinks the socket. Without this the session's exit
	// signal would leave one behind for the broker to dial and fail on.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go relay(ln, srv)
	return srv.Serve(ctx)
}

// enabledFor returns the owning session's pid, but only when that session
// enabled this channel. Claude Code spawns every configured MCP server and
// silently drops channel events for a session started without the flag, so a
// socket opened there would make the broker report undelivered pushes.
func enabledFor() int {
	table, err := proc.Snapshot()
	if err != nil {
		return 0
	}
	pid := table.NearestMatch(os.Getpid(), "claude")
	if pid == 0 {
		return 0
	}
	args, err := proc.Args(pid)
	if err != nil || !namesChannel(args) {
		return 0
	}
	return pid
}

// namesChannel reports whether argv opts this channel in. Claude Code takes
// names space-separated after either flag, as "server:<name>" or
// "plugin:<name>@<marketplace>".
func namesChannel(args string) bool {
	fields := strings.Fields(args)
	for i, f := range fields {
		flag, value, split := strings.Cut(f, "=")
		if flag != channelsFlag && flag != devChannelsFlag {
			continue
		}
		if split {
			if serverName(value) == Name {
				return true
			}
			continue
		}
		for _, v := range fields[i+1:] {
			if strings.HasPrefix(v, "-") {
				break
			}
			if serverName(v) == Name {
				return true
			}
		}
	}
	return false
}

func serverName(value string) string {
	_, name, ok := strings.Cut(value, ":")
	if !ok {
		return ""
	}
	name, _, _ = strings.Cut(name, "@")
	return name
}

func listen(pid int) (net.Listener, error) {
	path := ChannelSocket(pid)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// A socket left by a crashed run would block the bind.
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// relay turns each connection into one event, answering so the pusher knows
// whether to fall back.
func relay(ln net.Listener, srv *channelServer) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			var p struct {
				Content string            `json:"content"`
				Meta    map[string]string `json:"meta,omitempty"`
			}
			switch err := json.NewDecoder(io.LimitReader(conn, maxPayload)).Decode(&p); {
			case err != nil:
				fmt.Fprintf(conn, "err: %v", err)
			case p.Content == "":
				fmt.Fprint(conn, "err: empty content")
			default:
				if err := notify(srv, p.Content, p.Meta); err != nil {
					fmt.Fprintf(conn, "err: %v", err)
					return
				}
				fmt.Fprint(conn, "ok")
			}
		}()
	}
}

// notify shapes one channel event: content becomes the tag body, and each meta
// entry becomes a tag attribute.
func notify(srv *channelServer, content string, meta map[string]string) error {
	params := map[string]any{"content": content}
	if len(meta) > 0 {
		params["meta"] = meta
	}
	return srv.Notify(notifyMethod, params)
}
