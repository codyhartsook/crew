package cli

import (
	"io"
	"strings"
	"testing"
)

func TestPublicCommands(t *testing.T) {
	root := New()
	for _, name := range []string{"init", "uninstall", "ls", "whoami", "dashboard", "room", "post", "resolve", "remove", "state", "search", "promote"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name || cmd.Hidden {
			t.Errorf("public command %q = (%v, %v)", name, cmd, err)
		}
	}
	me, _, err := root.Find([]string{"me"})
	if err != nil || me.Name() != "whoami" {
		t.Errorf("me alias = (%v, %v), want whoami", me, err)
	}
	for _, name := range []string{"get", "inbox", "install", "join", "leave", "rm", "review", "version"} {
		if cmd, _, err := root.Find([]string{name}); err == nil && cmd.Name() == name {
			t.Errorf("removed command %q is still available", name)
		}
	}
	for _, name := range []string{"clear", "hook", "serve", "prune"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || !cmd.Hidden {
			t.Errorf("internal command %q = (%v, %v), want hidden", name, cmd, err)
		}
	}
}

func TestRoomAckRequiresInbox(t *testing.T) {
	if err := run(t, "room", "--ack"); err == nil || err.Error() != "--ack requires --inbox" {
		t.Errorf("room --ack error = %v, want --ack requires --inbox", err)
	}
}

func TestDashboardAliasesAndInitBrokerFlags(t *testing.T) {
	root := New()
	cmd, _, err := root.Find([]string{"fleet"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name() != "dashboard" || cmd.Hidden {
		t.Errorf("fleet = %q, hidden=%v", cmd.Name(), cmd.Hidden)
	}
	if f := cmd.Flags().Lookup("wake"); f != nil {
		t.Errorf("dashboard --wake = %v, want removed", f)
	}
	init, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"addr", "verbose"} {
		if f := init.Flags().Lookup(name); f == nil || f.Hidden {
			t.Errorf("init --%s = %v, want public flag", name, f)
		}
	}
}

func TestDashboardRequiresInit(t *testing.T) {
	err := run(t, "dashboard", "--addr", "127.0.0.1:0", "--no-open")
	if err == nil || !strings.Contains(err.Error(), "crew init") {
		t.Errorf("dashboard error = %v, want init guidance", err)
	}
}

// run executes one command through the root, which is where the flags are
// parsed and so the only place a command behaves as it does in the terminal.
func run(t *testing.T, args ...string) error {
	t.Helper()
	root := New()
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}
