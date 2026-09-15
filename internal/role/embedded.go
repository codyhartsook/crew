package role

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// embeddedFS holds the default roles shipped inside the crew binary.
//
//go:embed agents/*.toml
var embeddedFS embed.FS

// loadEmbedded reads every default role baked into the binary.
func loadEmbedded() ([]Definition, error) {
	fsys, err := fs.Sub(embeddedFS, "agents")
	if err != nil {
		return nil, fmt.Errorf("embedded agents: %w", err)
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded agents: %w", err)
	}

	var defs []Definition
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		def, err := load(fsys, e.Name(), ScopeEmbedded, "embedded:"+e.Name())
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, nil
}
