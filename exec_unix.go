//go:build !windows

package main

import (
	"os"
	"syscall"
)

func nodeExeName() string { return "node" }
func exeSuffix() string   { return "" }

// runArgv replaces the current process (zero overhead after lookup).
func runArgv(argv []string, env []string) int {
	if err := syscall.Exec(argv[0], argv, env); err != nil {
		// Only reached if exec fails (e.g. bad interpreter / ENOEXEC).
		os.Stderr.WriteString("pin: exec " + argv[0] + ": " + err.Error() + "\n")
		return 126
	}
	return 0
}
