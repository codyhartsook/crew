package initcmd

import (
	"os"
	"testing"
)

// Tests pass a temp home, so a CODEX_HOME from a Codex shell must not redirect them.
func TestMain(m *testing.M) {
	os.Unsetenv("CODEX_HOME")
	os.Exit(m.Run())
}
