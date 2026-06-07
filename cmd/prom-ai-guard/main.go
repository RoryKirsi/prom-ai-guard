// Command prom-ai-guard is the CLI entry point.
package main

import (
	"os"

	"prom-ai-guard/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
