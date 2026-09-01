package cli

import (
	"runtime/debug"
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
