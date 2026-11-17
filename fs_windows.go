//go:build windows

package main

import "os"

func readSmallFile(path string, buf []byte) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	n, err := f.Read(buf)
	f.Close()
	return n, err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
