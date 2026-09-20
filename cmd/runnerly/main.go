// Command runnerly manages self-hosted GitHub Actions runners.
package main

import (
	"os"

	"github.com/bablilayoub/runnerly/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
