package claude

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNamesChannel(t *testing.T) {
	for name, tc := range map[string]struct {
		args string
		want bool
	}{
		"development flag":  {"claude --dangerously-load-development-channels server:multiplayer", true},
		"channels flag":     {"claude --channels server:multiplayer", true},
		"plugin form":       {"claude --channels plugin:multiplayer@claude-plugins-official", true},
		"equals form":       {"claude --channels=server:multiplayer", true},
		"second in a list":  {"claude --channels server:other server:multiplayer", true},
		"no flag":           {"claude", false},
		"another channel":   {"claude --channels server:other", false},
		"name is a prefix":  {"claude --channels server:multiplayer-other", false},
		"bare name":         {"claude --channels multiplayer", false},
		"next arg is flag":  {"claude --channels --model haiku", false},
		"stops at next arg": {"claude --channels server:other --model server:multiplayer", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := namesChannel(tc.args); got != tc.want {
				t.Errorf("namesChannel(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// The handshake is the contract with Claude Code, so assert it through Serve.
// enabledFor finds no channels flag in a test's ancestry, so no socket opens.
func TestServeDeclaresTheChannel(t *testing.T) {
	var out strings.Builder
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28"}}`)
	if err := Serve(context.Background(), "0.0.1", in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m); err != nil {
		t.Fatalf("bad reply: %v", err)
	}
	result := m["result"].(map[string]any)
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["experimental"].(map[string]any)["claude/channel"]; !ok {
		t.Errorf("channel capability missing: %v", caps)
	}
	if _, ok := caps["tools"]; ok {
		t.Error("declared tools; this channel is one-way")
	}
	// The rejected revision must not be echoed back.
	if result["protocolVersion"] == "2026-07-28" {
		t.Error("negotiated a revision Claude Code will not register")
	}
	if result["serverInfo"].(map[string]any)["name"] != Name {
		t.Errorf("serverInfo = %v", result["serverInfo"])
	}
	if !strings.Contains(result["instructions"].(string), "multiplayer room --inbox --ack") {
		t.Errorf("instructions = %q", result["instructions"])
	}
}

func TestChannelServerHandlesPingAndUnknownMethod(t *testing.T) {
	var out strings.Builder
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n"))
	if err := newChannelServer("0.0.1", in, &out).Serve(context.Background()); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var reply map[string]any
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("bad reply: %v", err)
		}
		replies = append(replies, reply)
	}
	if len(replies) != 2 || replies[0]["error"] != nil || replies[1]["error"] == nil {
		t.Errorf("replies = %v", replies)
	}
}
