// Command telescope inventories cloud workloads and recommends Containarium
// consolidation to cut cost.
package main

import (
	"os"

	"github.com/footprintai/telescope/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
