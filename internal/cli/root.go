// Package cli wires the cobra command tree for prom-ai-guard.
//
// Slices ship scan and gate. The contract (§3) defines further subcommands —
// relabel, diff, doctor — which later slices register here.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// exitCodeError lets a command request a specific process exit code (the gate
// uses code 2 for a policy failure). Plain errors map to exit code 1.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

// NewRootCmd builds the root command with all registered subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "prom-ai-guard",
		Short:         "AI-driven Prometheus invalid-metric governance tool",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCmd())
	root.AddCommand(newGateCmd())
	root.AddCommand(newRelabelCmd())
	root.AddCommand(newDiffCmd())
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	return run(os.Args[1:], os.Stdout, os.Stderr)
}

// run executes the command tree with the given args/streams and maps the result
// to an exit code: 0 success, 2 (or other) from an exitCodeError, else 1.
func run(args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		return 0
	}
	var ec *exitCodeError
	if errors.As(err, &ec) {
		// The command already wrote its own output; do not print to stderr.
		return ec.code
	}
	fmt.Fprintf(stderr, "prom-ai-guard: %v\n", err)
	return 1
}
