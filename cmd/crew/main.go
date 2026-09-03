// Command crew records and reports which coding-agent sessions are running
// in which git repositories and worktrees.
package main

import (
	"os"

	"github.com/codyhartsook/multiplayer/internal/cli"
)

func main() { os.Exit(cli.Execute()) }
