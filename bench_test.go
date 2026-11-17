package main

// Why pin keeps a single JSON file instead of a database, in numbers:
//
//	go test -run xxx -bench BenchmarkLoad .
//
// The registry is read only by the CLI, never by a shim (a shim reads one
// 200-byte metadata file), so the size that matters is "how slow does `pin ls`
// get". At 500 entries it is under a millisecond; at 5000 it is ~9ms. A SQLite
// driver would cost either CGO (and the cross-compiled release build) or a
// dozen transitive dependencies, to fix a write that writeFileAtomic makes
// atomic in six lines.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func fill(t testing.TB, n int) {
	t.Setenv("PIN_HOME", t.TempDir())
	db := map[string]Entry{}
	for i := 0; i < n; i++ {
		k := fmt.Sprintf("tool%03d", i)
		db[k] = Entry{
			Spec:     "@scope/" + k,
			Version:  "1.2.3",
			Runtime:  "20.20.2",
			NodePath: "C:/Users/someone/scoop/apps/fnm/current/node-versions/v20.20.2/installation/node.exe",
			Bins:     []string{k, k + "-x", k + "-y"},
		}
	}
	if err := saveDB(db); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dbPath())
	t.Logf("entries=%4d  pins.json=%7d bytes", n, st.Size())
}

func benchLoad(b *testing.B, n int) {
	fill(b, n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loadDB(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoad1(b *testing.B)    { benchLoad(b, 1) }
func BenchmarkLoad50(b *testing.B)   { benchLoad(b, 50) }
func BenchmarkLoad500(b *testing.B)  { benchLoad(b, 500) }
func BenchmarkLoad5000(b *testing.B) { benchLoad(b, 5000) }

// The shim's own path, for scale: read a 200-byte metadata file.
func BenchmarkReadShimFile(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("PIN_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		b.Fatal(err)
	}
	body := "pin1\nnode\nC:/n/node.exe\nC:/e.js\nni\n20.20.2\n"
	if err := os.WriteFile(filepath.Join(dir, "tools", "ni.txt"), []byte(body), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := readShimFile("ni"); err != nil {
			b.Fatal(err)
		}
	}
}
