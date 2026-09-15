package hookcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/store/httpstore"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func hookLog(t *testing.T, dbDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dbDir, "hook.log"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A store that cannot inject a roster or enforce a role fails open rather
// than breaking the harness, so "nothing happened" needs a reason findable
// somewhere - CREW_DEBUG=1 and the hook log, not silence all the way down.
func TestLogMissingCapabilitiesRecordsWhatAStoreLacks(t *testing.T) {
	dir := t.TempDir()
	opts := &cmdutil.Options{DB: filepath.Join(dir, "sessions.db")}
	st := httpstore.New("http://example.invalid")

	logMissingCapabilities(opts, st)

	log := hookLog(t, dir)
	for _, want := range []string{"rooms", "roles", "routing", "delegation"} {
		if !strings.Contains(log, want) {
			t.Errorf("hook.log = %q, want it to name the missing %q capability", log, want)
		}
	}
}

func TestLogMissingCapabilitiesIsSilentForAFullyCapableStore(t *testing.T) {
	dir := t.TempDir()
	opts := &cmdutil.Options{DB: filepath.Join(dir, "sessions.db")}
	st, err := sqlitestore.Open(opts.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	logMissingCapabilities(opts, st)

	if log := hookLog(t, dir); log != "" {
		t.Errorf("hook.log = %q, want nothing logged for a store missing nothing", log)
	}
}
