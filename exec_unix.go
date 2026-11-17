//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

func nodeExeName() string { return "node" }
func exeSuffix() string   { return "" }

// runArgv replaces the current process, so it only ever returns on failure. The
// second result says whether the failure was "that file is not there", which is
// the one the caller can do something about.
func runArgv(argv []string, env []string) (int, bool) {
	if err := syscall.Exec(argv[0], argv, env); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, false
		}
		// Bad interpreter, permission, wrong architecture: already reported.
		os.Stderr.WriteString("pin: exec " + argv[0] + ": " + err.Error() + "\n")
		return 126, true
	}
	return 0, true
}
