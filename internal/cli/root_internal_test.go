package cli

import "testing"

func TestPublicCommands(t *testing.T) {
	root := New()
	for _, name := range []string{"install", "uninstall", "ls", "fleet", "room", "post", "resolve", "state", "search", "promote"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name || cmd.Hidden {
			t.Errorf("public command %q = (%v, %v)", name, cmd, err)
		}
	}
	for _, name := range []string{"clear", "get", "inbox", "join", "leave", "rm", "review", "version"} {
		if cmd, _, err := root.Find([]string{name}); err == nil && cmd.Name() == name {
			t.Errorf("removed command %q is still available", name)
		}
	}
	for _, name := range []string{"hook", "serve", "prune"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || !cmd.Hidden {
			t.Errorf("internal command %q = (%v, %v), want hidden", name, cmd, err)
		}
	}
}

func TestRoomAckRequiresInbox(t *testing.T) {
	cmd := newRoomCmd(&options{})
	cmd.SetArgs([]string{"--ack"})
	if err := cmd.Execute(); err == nil || err.Error() != "--ack requires --inbox" {
		t.Errorf("room --ack error = %v, want --ack requires --inbox", err)
	}
}
