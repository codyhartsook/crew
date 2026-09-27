package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/detect"
)

func newAnchor(_ *cmdutil.Options) *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "anchor [dir]",
		Short: "Mark a folder as a room root",
		Long: `Writes the ` + detect.FolderMarker + ` marker, so every agent started under this
folder shares one room instead of one room per directory.

Checkouts need no marker: a repository already anchors its own rooms, and a
marker inside one is ignored.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if err := anchor(root, name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "anchored %s as room %q\n", root, roomName(root, name))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "room name, if not the directory name")
	return cmd
}

// anchor is idempotent: re-anchoring only rewrites the name, and dropping the
// flag leaves a name already recorded alone.
func anchor(root, name string) error {
	marker := filepath.Join(root, detect.FolderMarker)
	if err := os.MkdirAll(marker, 0o755); err != nil {
		return fmt.Errorf("create marker: %w", err)
	}
	if name == "" {
		return nil
	}
	data, err := json.MarshalIndent(struct {
		Name string `json:"name"`
	}{name}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(marker, detect.FolderConfig), append(data, '\n'), 0o644)
}

func roomName(root, name string) string {
	if name != "" {
		return name
	}
	return filepath.Base(root)
}
