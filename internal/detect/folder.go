package detect

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/codyhartsook/multiplayer/internal/session"
)

const (
	// FolderMarker anchors a plain folder as a room root. Every session under
	// it shares one room, the way a checkout's sessions share the repo's.
	FolderMarker = ".crew"
	// FolderConfig optionally names the room; without it the directory does.
	FolderConfig = "room.json"
)

// folderAnchor recognizes a plain folder marked as a room root.
type folderAnchor struct{}

func (folderAnchor) Name() string { return "folder" }

// Lookup walks up for the marker, stopping at the home directory. A marker at the
// filesystem root is ignored: it would pull every folder into one room.
func (folderAnchor) Lookup(_ context.Context, dir string) (*session.Place, error) {
	root := normalize(dir)
	if root == "" {
		return nil, nil
	}
	stop := normalize(homeDir())

	for d := root; ; {
		parent := filepath.Dir(d)
		atFSRoot := parent == d
		if !atFSRoot && marked(d) {
			return &session.Place{Folder: &session.Folder{Name: folderName(d), Root: d}}, nil
		}
		if atFSRoot || d == stop {
			return nil, nil
		}
		d = parent
	}
}

func marked(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FolderMarker))
	return err == nil
}

// folderName prefers the name recorded in the marker, so two folders that share
// a leaf name are still told apart.
func folderName(root string) string {
	data, err := os.ReadFile(filepath.Join(root, FolderMarker, FolderConfig))
	if err == nil {
		var cfg struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &cfg) == nil && cfg.Name != "" {
			return cfg.Name
		}
	}
	return filepath.Base(root)
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
