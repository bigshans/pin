//go:build windows

package main

import (
	"os"
	"os/exec"
)

func nodeExeName() string { return "node.exe" }
func exeSuffix() string   { return ".exe" }

// runArgv spawns the child and waits. Windows has no exec(2), so one extra
// process is unavoidable; stdio is inherited and the console is shared, so
// Ctrl+C reaches the child too.
func runArgv(argv []string, env []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		os.Stderr.WriteString("pin: " + err.Error() + "\n")
		return 126
	}
	return 0
}
