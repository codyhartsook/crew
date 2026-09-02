package claude

import (
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
)

// fakeChannel answers one connection the way the channel server does.
func fakeChannel(t *testing.T, reply string) (string, <-chan string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "c.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var p struct {
			Content string `json:"content"`
		}
		_ = json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&p)
		got <- p.Content
		_, _ = conn.Write([]byte(reply))
	}()
	return sock, got
}

func TestPush(t *testing.T) {
	sock, got := fakeChannel(t, "ok")
	if err := push(sock, "question [7]"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if content := <-got; content != "question [7]" {
		t.Errorf("delivered %q", content)
	}
}

func TestPushRefused(t *testing.T) {
	sock, _ := fakeChannel(t, "err: dropped")
	if err := push(sock, "hi"); err == nil {
		t.Fatal("want an error when the channel refuses")
	}
}

func TestPushWithoutAChannel(t *testing.T) {
	if err := push(filepath.Join(t.TempDir(), "none.sock"), "hi"); err == nil {
		t.Fatal("want an error when no channel is listening")
	}
}

func TestChannelSocketKeyedByPID(t *testing.T) {
	if got := ChannelSocket(4242); got != socketDir+"/4242.sock" {
		t.Errorf("ChannelSocket(4242) = %q", got)
	}
}
