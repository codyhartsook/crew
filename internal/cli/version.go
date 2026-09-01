package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// version is set with -ldflags "-X ...cli.version=v1.2.3"; otherwise it comes
// from the build's VCS stamp.
var version = ""

// Version reports what this binary is.
func Version() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			if len(setting.Value) > 12 {
				return setting.Value[:12]
			}
			return setting.Value
		}
	}
	return "dev"
}

func newVersionCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, schema version and store path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := opts.dbPath()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "multiplayer %s\nschema     %d\nstore      %s\n",
				Version(), sqlitestore.SchemaVersion, db)
			return nil
		},
	}
}
