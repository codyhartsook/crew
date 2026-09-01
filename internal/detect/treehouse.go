package detect

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/codyhartsook/multiplayer/internal/session"
)

const (
	// treehouseStateFile is the pool manifest treehouse maintains next to its
	// numbered worktree slots.
	treehouseStateFile = "treehouse-state.json"
	// treehouseMaxAscent bounds the walk toward the filesystem root. A pool
	// worktree sits two levels below its manifest; the headroom covers
	// in-project pools.
	treehouseMaxAscent = 6
)

// treehouseState mirrors the parts of treehouse-state.json this tool reads.
type treehouseState struct {
	Worktrees []struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Leased      bool   `json:"leased"`
		LeaseID     string `json:"lease_id"`
		LeaseHolder string `json:"lease_holder"`
	} `json:"worktrees"`
}

// treehouseFor reports the pool slot that owns worktreeRoot, or (nil, nil) for
// an ordinary checkout. It walks up to the manifest and matches on the recorded
// path rather than pattern-matching TREEHOUSE_ROOT, so a pool relocated with
// --root, or an in-project one, is still found.
func treehouseFor(worktreeRoot string) (*session.Treehouse, error) {
	root := normalize(worktreeRoot)
	if root == "" {
		return nil, nil
	}

	dir := root
	for i := 0; i < treehouseMaxAscent; i++ {
		manifest := filepath.Join(dir, treehouseStateFile)
		if _, err := os.Stat(manifest); err == nil {
			return matchManifest(manifest, dir, root)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, nil
}

// matchManifest reads a pool manifest and returns the slot whose recorded path
// is worktreeRoot. Finding the manifest but no matching slot is not an error:
// the checkout sits under a pool directory without being one of its worktrees.
func matchManifest(manifest, poolDir, worktreeRoot string) (*session.Treehouse, error) {
	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil, err
	}
	var state treehouseState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	for _, wt := range state.Worktrees {
		if normalize(wt.Path) != worktreeRoot {
			continue
		}
		return &session.Treehouse{
			Pool:        filepath.Base(poolDir),
			Slot:        wt.Name,
			Root:        worktreeRoot,
			Leased:      wt.Leased,
			LeaseID:     wt.LeaseID,
			LeaseHolder: wt.LeaseHolder,
		}, nil
	}
	return nil, nil
}
