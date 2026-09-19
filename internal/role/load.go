package role

import (
	"context"
	"fmt"
	"io/fs"
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

// RepoDir is where a repo's own role definitions live, relative to its root.
func RepoDir(repoRoot string) string { return filepath.Join(repoRoot, dirName) }

// Load reads every *.toml file in dir as a validated definition. A missing
// directory is not an error: most scopes define no roles.
func Load(dir string, scope Scope) ([]Definition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	fsys := os.DirFS(dir)
	var defs []Definition
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		def, err := load(fsys, e.Name(), scope, filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, nil
}

// load decodes one definition out of fsys; displayPath need not be a real path.
func load(fsys fs.FS, name string, scope Scope, displayPath string) (Definition, error) {
	var def Definition
	meta, err := toml.DecodeFS(fsys, name, &def)
	if err != nil {
		return Definition{}, fmt.Errorf("%s: %w", displayPath, err)
	}
	def.Scope = scope
	def.Path = displayPath
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Definition{}, fmt.Errorf("%s: unknown field(s): %s", displayPath, strings.Join(keys, ", "))
	}
	if err := def.Validate(); err != nil {
		return Definition{}, err
	}
	return def, nil
}

// Registry is every role definition in effect, repo already won on collision.
type Registry struct {
	byName   map[string]Definition
	shadowed map[string]Definition
}

// Discover loads both scopes and merges them. repoDir may be empty.
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

// DiscoverFor is Discover given a known repo root (empty for none), with
// embedded default roles filled in under anything repo or global defines.
func DiscoverFor(repoRoot string) (*Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	repoDir := ""
	if repoRoot != "" {
		repoDir = RepoDir(repoRoot)
	}
	reg, err := Discover(repoDir, GlobalDir(home))
	if err != nil {
		return nil, err
	}

	embedded, err := loadEmbedded()
	if err != nil {
		return nil, err
	}
	for _, d := range embedded {
		if _, ok := reg.byName[d.Name]; ok {
			continue
		}
		reg.byName[d.Name] = d
	}
	return reg, nil
}

// DiscoverFromDir is DiscoverFor, detecting the repo root from dir first.
// Prefer DiscoverFor when the root is already known: this shells out to git.
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

// Shadowed reports the global definition a repo one of the same name hid.
func (r *Registry) Shadowed(name string) (Definition, bool) {
	d, ok := r.shadowed[name]
	return d, ok
}
