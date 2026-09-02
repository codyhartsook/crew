package claude

import "testing"

func TestEntryRunsTheSubcommand(t *testing.T) {
	e := Entry("/opt/bin/multiplayer")
	if e["command"] != "/opt/bin/multiplayer" {
		t.Errorf("command = %v", e["command"])
	}
	if args, ok := e["args"].([]any); !ok || len(args) != 1 || args[0] != Subcommand {
		t.Errorf("args = %v", e["args"])
	}
}

func TestIsOurs(t *testing.T) {
	for name, tc := range map[string]struct {
		entry any
		want  bool
	}{
		"ours, any path": {Entry("/somewhere/else/multiplayer"), true},
		"another server": {map[string]any{"command": "x", "args": []any{"serve"}}, false},
		"no args":        {map[string]any{"command": "x"}, false},
		"too many args":  {map[string]any{"args": []any{"channel", "--x"}}, false},
		"not an object":  {"nonsense", false},
		"nothing there":  {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IsOurs(tc.entry); got != tc.want {
				t.Errorf("IsOurs(%v) = %v, want %v", tc.entry, got, tc.want)
			}
		})
	}
}
