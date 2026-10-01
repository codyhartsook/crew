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
	// treehouseMaxAscent bounds the walk to the root: a pool worktree sits two
	// levels below its manifest, with headroom for in-project pools.
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

// treehouse reads the pool manifest treehouse keeps beside its numbered slots.
type treehouse struct{}

func (treehouse) Name() string { return "treehouse" }

// Lookup walks up to the manifest and matches the recorded path, not the
// TREEHOUSE_ROOT pattern, so relocated or in-project pools are still found.
func (t treehouse) Lookup(worktreeRoot string) (*session.Pool, error) {
	root := normalize(worktreeRoot)
	if root == "" {
		return nil, nil
	}

	dir := root
	for i := 0; i < treehouseMaxAscent; i++ {
		manifest := filepath.Join(dir, treehouseStateFile)
		if _, err := os.Stat(manifest); err == nil {
			return t.match(manifest, dir, root)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, nil
}

// match returns the slot recorded at worktreeRoot. No match is not an error: the
// checkout sits under a pool directory without being one of its worktrees.
func (t treehouse) match(manifest, poolDir, worktreeRoot string) (*session.Pool, error) {
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
		return &session.Pool{
			Manager:     t.Name(),
			Name:        filepath.Base(poolDir),
			Slot:        wt.Name,
			Root:        worktreeRoot,
			Leased:      wt.Leased,
			LeaseID:     wt.LeaseID,
			LeaseHolder: wt.LeaseHolder,
		}, nil
	}
	return nil, nil
}
