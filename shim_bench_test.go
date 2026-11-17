//go:build !windows

package main

import (
	"testing"
)

// benchShim installs a self-contained shim (its runtime and entry both point at
// /bin/true, which exists on every unix) and returns its bin name.
func benchShim(b *testing.B) string {
	b.Helper()
	home := b.TempDir()
	b.Setenv("PIN_HOME", home)
	const bin = "probe"
	if err := writeShimFile(bin, "node", "/bin/true", "/bin/true", "probe", "26.13.1"); err != nil {
		b.Fatal(err)
	}
	return bin
}

// BenchmarkShimPlan measures everything a shim does before Exec: read six lines
// of metadata, confirm runtime and entry, build argv and env. runShim adds one
// syscall (exec) on top of this.
func BenchmarkShimPlan(b *testing.B) {
	bin := benchShim(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sh, err := readShimFile(bin)
		if err != nil {
			b.Fatal(err)
		}
		argv, env, problem := planShim(sh, bin, []string{"--version"})
		if problem != "" {
			b.Fatal(problem)
		}
		if len(argv) != 3 || len(env) == 0 {
			b.Fatal("bad plan")
		}
	}
}
