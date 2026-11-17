// Command downloadx is a fast, multi-connection download manager.
package main

import (
	"os"

	"github.com/neko233-com/DownloadX/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
