package main

import (
	"embed"
	"io/fs"
	"os"

	"openlia/cli"
)

// The release payload is embedded so the operator can bootstrap a target
// without cloning this repository on the operator or remote machine.
//
//go:embed docker ops profile workspace-template locho release
var releaseAssets embed.FS

func main() {
	assets, err := fs.Sub(releaseAssets, ".")
	if err != nil {
		os.Exit(cli.ExitInternal)
	}
	os.Exit(cli.Run(os.Args[1:], assets))
}
