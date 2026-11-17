//go:build windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
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
func runArgv(argv []string, env []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		os.Stderr.WriteString("pin: " + err.Error() + "\n")
		return 126
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
			return exitCode(err)
		case <-interrupts:
			if deadline != nil {
				// Second Ctrl+C: the child is not going to stop on its own.
				return killAndReap(cmd, done)
			}
			t := time.NewTimer(graceInterrupts)
			defer t.Stop()
			deadline = t.C
		case <-deadline:
			return killAndReap(cmd, done)
		}
	}
}

func killAndReap(cmd *exec.Cmd, done <-chan error) int {
	killTree(cmd.Process)
	<-done
	return exitInterrupted
}

// killTree terminates the child and everything it spawned. The direct child is
// usually a launcher: node running a script that shells out to npm, which
// starts another node, so Process.Kill alone would leave the real work alive.
func killTree(p *os.Process) {
	tk := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid))
	tk.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := tk.Run(); err == nil {
		return
	}
	_ = p.Kill()
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
