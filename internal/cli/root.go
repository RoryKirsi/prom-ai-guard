// Package cli wires the cobra command tree for prom-ai-guard.
//
// Slice 1 ships only the `scan` subcommand. The contract (§3) defines further
// subcommands — gate, relabel, diff, doctor — which later slices register here.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the root command with all registered subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "prom-ai-guard",
		Short:         "AI-driven Prometheus invalid-metric governance tool",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCmd())
	return root
}

// Execute runs the root command and returns the process exit code.
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "prom-ai-guard: %v\n", err)
		return 1 // tool error (contract: exit code 1)
	}
	return 0
}
