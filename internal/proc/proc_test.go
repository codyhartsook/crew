package proc_test

import (
	"os"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/proc"
)

func TestSnapshotFindsThisProcess(t *testing.T) {
	table, err := proc.Snapshot()
	if err != nil {
		t.Skipf("process inspection unavailable: %v", err)
	}
	me := os.Getpid()
	if !table.Running(me, "") {
		t.Fatalf("snapshot does not contain this process (%d)", me)
	}
	if !table.Running(me, "proc.test") {
		t.Errorf("command for pid %d = %q, want the test binary", me, table[me].Command)
	}
	if table.Running(me, "definitely-not-this") {
		t.Error("Running matched a command that is not ours")
	}
}

func TestAncestorsReachesTheParent(t *testing.T) {
	table, err := proc.Snapshot()
	if err != nil {
		t.Skipf("process inspection unavailable: %v", err)
	}
	chain := table.Ancestors(os.Getpid())
	if len(chain) < 2 || chain[0] != os.Getpid() {
		t.Fatalf("ancestors = %v, want this process first then its forebears", chain)
	}
	if chain[1] != os.Getppid() {
		t.Errorf("ancestors[1] = %d, want the parent %d", chain[1], os.Getppid())
	}
}

func TestNearestMatch(t *testing.T) {
	table := proc.Table{
		10: {PID: 10, PPID: 20, Command: "multiplayer"},
		20: {PID: 20, PPID: 30, Command: "sh"},
		30: {PID: 30, PPID: 1, Command: "codex"},
	}
	if got := table.NearestMatch(10, "codex"); got != 30 {
		t.Errorf("NearestMatch = %d, want 30 through the intermediate shell", got)
	}
	if got := table.NearestMatch(10, "claude"); got != 0 {
		t.Errorf("NearestMatch = %d, want 0 when no ancestor matches", got)
	}
}

func TestRunningOnMissingPID(t *testing.T) {
	if (proc.Table{}).Running(1234, "") {
		t.Error("Running reported a pid absent from the table as live")
	}
}
