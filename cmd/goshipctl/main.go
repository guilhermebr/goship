package main

import (
	"os"

	"github.com/guilhermebr/goship/cmd/goshipctl/commands"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	commands.SetVersion(Version, Commit, BuildTime)

	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
