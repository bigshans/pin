//go:build windows

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"time"
)

func nodeExeName() string { return "node.exe" }
func exeSuffix() string   { return ".exe" }

// exitInterrupted is what a Unix shell reports for a process killed by SIGINT.
const exitInterrupted = 130

// graceInterrupts is how long the child gets to shut down by itself after the
// console broadcast, before we stop waiting and kill it.
const graceInterrupts = 5 * time.Second

// runArgv spawns the child and waits. Windows has no exec(2), so one extra
// process is unavoidable; stdio is inherited and the console is shared, so the
// console delivers Ctrl+C to the child (and to everything the child spawned)
// on its own.
//
// What the shim must not do is die first: Go's default console handler exits
// the process on CTRL_C_EVENT, so a shim that ignores the signal vanishes the
// moment Ctrl+C is pressed, leaving the tool it started running and holding the
// terminal. Registering a handler keeps the shim around to reap the child, and
// a second Ctrl+C (or the grace timeout) kills it instead of waiting forever.
//
// The other half is the job object (job_windows.go). The tool is a chain — pin,
// node, cmd.exe, node — and Ctrl+C only reaches it because the console
// broadcasts. The job covers the cases where nothing is left to broadcast: pin
// killed from Task Manager, or the terminal window closed, where Windows closes
// our handles for us. KILL_ON_JOB_CLOSE then takes the whole tree down.
func runArgv(argv []string, env []string) (int, bool) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Create the job before the child exists; a process cannot be pulled into a
	// job together with children it made earlier. When this fails we still run
	// the tool, just without the guarantee that its tree dies with us.
	j, jerr := newKillOnCloseJob()

	if err := cmd.Start(); err != nil {
		if j != nil {
			j.close()
		}
		if errors.Is(err, fs.ErrNotExist) {
			return 0, false
		}
		os.Stderr.WriteString("pin: " + err.Error() + "\n")
		return 126, true
	}
	if jerr != nil {
		j = nil
		fmt.Fprintf(os.Stderr, "pin: no job object (%v); only the tool itself will be killed\n", jerr)
	} else if err := j.assign(cmd.Process); err != nil {
		os.Stderr.WriteString("pin: " + err.Error() + "\n")
		j.close()
		j = nil
	}
	if j != nil {
		defer j.close()
	}

	// CTRL_BREAK_EVENT arrives as os.Interrupt too: the runtime maps both console
	// events to SIGINT.
	interrupts := make(chan os.Signal, 8)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var deadline <-chan time.Time
	for {
		select {
		case err := <-done:
			return exitCode(err), true
		case <-interrupts:
			if deadline != nil {
				// Second Ctrl+C: the child is not going to stop on its own.
				return killAndReap(cmd, done, j), true
			}
			t := time.NewTimer(graceInterrupts)
			defer t.Stop()
			deadline = t.C
		case <-deadline:
			return killAndReap(cmd, done, j), true
		}
	}
}

func killAndReap(cmd *exec.Cmd, done <-chan error, j *job) int {
	if j != nil {
		_ = j.terminate()
	} else {
		// No job: this is as far as we can reach. The tool usually spawns more
		// processes, and they will survive.
		_ = cmd.Process.Kill()
	}
	<-done
	return exitInterrupted
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
	}
	os.Stderr.WriteString("pin: " + err.Error() + "\n")
	return 126
}
