//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This file is the acceptance test for job_windows.go. Run:
//
//	go test -run TestJob -v .
//
// Both tests spawn a two level tree that mimics pin -> node(ni) -> node(script)
// and check that the grandchild dies too — the whole point of the exercise.

func procAlive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// spawnTree starts node running a script that spawns a grandchild 400ms later.
// The delay matters: a child created *before* the parent joins the job stays
// outside it, so the test must give the assignment time to win — exactly the
// race documented on job.assign.
func spawnTree(t *testing.T) (cmd *exec.Cmd, child, grand int) {
	t.Helper()
	node, err := exec.LookPath(nodeExeName())
	if err != nil {
		t.Skip("node not on PATH")
	}
	dir := t.TempDir()
	childPid := filepath.Join(dir, "child.pid")
	grandPid := filepath.Join(dir, "grand.pid")
	grandJs := filepath.Join(dir, "grand.js")
	treeJs := filepath.Join(dir, "tree.js")

	must := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(grandJs, `require('fs').writeFileSync(process.argv[2], ''+process.pid)
setInterval(() => {}, 1000)`)
	must(treeJs, `const {spawn} = require('child_process')
require('fs').writeFileSync(process.argv[2], ''+process.pid)
setTimeout(() => {
  spawn(process.execPath, [process.argv[3], process.argv[4]], {stdio: 'ignore'})
}, 400)
setInterval(() => {}, 1000)`)

	cmd = exec.Command(node, treeJs, childPid, grandJs, grandPid)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	readPid := func(path string) int {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(path); err == nil {
				if n, _ := strconv.Atoi(strings.TrimSpace(string(b))); n > 0 {
					return n
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%s never appeared", filepath.Base(path))
		return 0
	}
	child = readPid(childPid)
	grand = readPid(grandPid)
	if !procAlive(child) || !procAlive(grand) {
		t.Fatalf("tree not alive before the test starts: child=%v grand=%v",
			procAlive(child), procAlive(grand))
	}
	return cmd, child, grand
}

func waitGone(t *testing.T, cmd *exec.Cmd, pids ...int) {
	t.Helper()
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the direct child never exited")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, p := range pids {
			if procAlive(p) {
				alive = true
			}
		}
		if !alive {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, p := range pids {
		if procAlive(p) {
			t.Errorf("pid %d survived", p)
		}
	}
}

func TestJobTerminateKillsTree(t *testing.T) {
	j, err := newKillOnCloseJob()
	if err != nil {
		t.Fatalf("newKillOnCloseJob: %v", err)
	}
	defer j.close()

	cmd, child, grand := spawnTree(t)
	if err := j.assign(cmd.Process); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := j.terminate(); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	waitGone(t, cmd, child, grand)
}

func TestJobCloseKillsTree(t *testing.T) {
	j, err := newKillOnCloseJob()
	if err != nil {
		t.Fatalf("newKillOnCloseJob: %v", err)
	}

	cmd, child, grand := spawnTree(t)
	if err := j.assign(cmd.Process); err != nil {
		t.Fatalf("assign: %v", err)
	}
	// Closing the last handle is what stands in for "pin was killed": Windows
	// closes our handles when the process dies, so the tree must die with us.
	j.close()
	waitGone(t, cmd, child, grand)
}
