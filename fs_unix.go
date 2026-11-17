//go:build !windows

package main

import (
	"os"
	"syscall"
)

// readSmallFile reads up to len(buf) bytes with the three syscalls it takes:
// open, read, close. os.Open would add an *os.File (and its finalizer) to a path
// that runs before every invocation of a pinned tool.
//
// One read is enough for a regular file, which returns everything asked for
// unless it hits end of file.
func readSmallFile(path string, buf []byte) (int, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return 0, &os.PathError{Op: "open", Path: path, Err: err}
	}
	n, err := syscall.Read(fd, buf)
	syscall.Close(fd)
	if err != nil {
		return 0, &os.PathError{Op: "read", Path: path, Err: err}
	}
	return n, nil
}

// fileExists is os.Stat without the FileInfo: the shim only needs to know
// whether the pinned path is still there before handing over.
func fileExists(path string) bool {
	return syscall.Access(path, 0) == nil // F_OK
}
