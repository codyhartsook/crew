package role

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/codyhartsook/multiplayer/internal/detect"
)

// dirName is where each scope keeps its role definitions.
const dirName = ".crew/agents"

// GlobalDir is where user-wide role definitions live, relative to home.
func GlobalDir(home string) string { return filepath.Join(home, dirName) }

// RepoDir is where a repository's own role definitions live, relative to its
// root.
func RepoDir(repoRoot string) string { return filepath.Join(repoRoot, dirName) }

// Load reads every *.toml file directly in dir as a definition of the given
// scope. A missing directory is not an error: most scopes define no roles.
// Every definition is validated before Load returns, so a bad file is caught
// here rather than at spawn time, and the error names it.
func Load(dir string, scope Scope) ([]Definition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var defs []Definition
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		def, err := load(path, scope)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, nil
}

func load(path string, scope Scope) (Definition, error) {
	var def Definition
	meta, err := toml.DecodeFile(path, &def)
	if err != nil {
		return Definition{}, fmt.Errorf("%s: %w", path, err)
	}
	def.Scope = scope
	def.Path = path
	def.normalize()

	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Definition{}, fmt.Errorf("%s: unknown field(s): %s", path, strings.Join(keys, ", "))
	}
	if err := def.Validate(); err != nil {
		return Definition{}, err
	}
	return def, nil
}

// Registry is every role definition currently in effect: repo and global,
// with repo already resolved as the winner of any name collision.
type Registry struct {
	byName   map[string]Definition
	shadowed map[string]Definition
}

// Discover loads both scopes and merges them. repoDir may be empty, for a
// command running outside any repository.
func Discover(repoDir, globalDir string) (*Registry, error) {
	global, err := Load(globalDir, ScopeGlobal)
	if err != nil {
		return nil, err
	}
	var repo []Definition
	if repoDir != "" {
		repo, err = Load(repoDir, ScopeRepo)
		if err != nil {
			return nil, err
		}
	}

	reg := &Registry{byName: map[string]Definition{}, shadowed: map[string]Definition{}}
	for _, d := range global {
		reg.byName[d.Name] = d
	}
	for _, d := range repo {
		if existing, ok := reg.byName[d.Name]; ok && existing.Scope == ScopeGlobal {
			reg.shadowed[d.Name] = existing
		}
		reg.byName[d.Name] = d
	}
	return reg, nil
}

// DiscoverFor is Discover resolved from a repo root a caller already knows
// (empty for none), and the user's home for the global scope. The one glue
// every caller needs, so it exists in one place instead of several.
func DiscoverFor(repoRoot string) (*Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	repoDir := ""
	if repoRoot != "" {
		repoDir = RepoDir(repoRoot)
	}
	return Discover(repoDir, GlobalDir(home))
}

// DiscoverFromDir is DiscoverFor, detecting the repo root from dir first.
// dir need not be the caller's own cwd - the broker resolves a delegation's
// stored directory this same way. Prefer DiscoverFor when the repo root is
// already known: detection shells out to git.
func DiscoverFromDir(ctx context.Context, dir string) (*Registry, error) {
	repoRoot := ""
	if place, err := detect.New().Detect(ctx, dir); err == nil && place != nil && place.Repo != nil {
		repoRoot = place.Repo.Root
	}
	return DiscoverFor(repoRoot)
}

// All lists every active definition, alphabetically by name.
func (r *Registry) All() []Definition {
	out := make([]Definition, 0, len(r.byName))
	for _, d := range r.byName {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get looks up one role by name.
func (r *Registry) Get(name string) (Definition, bool) {
	d, ok := r.byName[name]
	return d, ok
}

// Shadowed reports the global definition a repo one of the same name hid, if
// any, so a listing can say why only one is active.
func (r *Registry) Shadowed(name string) (Definition, bool) {
	d, ok := r.shadowed[name]
	return d, ok
}
