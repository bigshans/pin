package main

import (
	"os"
	"strings"
	"testing"
)

func TestShimFileRoundTrip(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())

	if err := writeShimFile("ni", "node", `C:\node\node.exe`, `C:\pkgs\ni\bin\ni.mjs`, "ni", "20.20.2"); err != nil {
		t.Fatal(err)
	}
	got, err := readShimFile("ni")
	if err != nil {
		t.Fatal(err)
	}
	want := shimInfo{Kind: "node", Node: `C:\node\node.exe`, Entry: `C:\pkgs\ni\bin\ni.mjs`, Tool: "ni", Version: "20.20.2"}
	if *got != want {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
}

// The metadata is read on every invocation of a pinned tool, so a file that
// grew past the bound must be refused rather than loaded.
func TestShimFileTooLarge(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())

	body := strings.Join([]string{shimMagic, "node", `C:\node\node.exe`, `C:\e.js`, "ni", "20.20.2"}, "\n") + "\n"
	big := body + strings.Repeat("#", maxShimFile)
	if err := os.MkdirAll(toolsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shimPath("ni"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readShimFile("ni"); err == nil {
		t.Fatal("expected the oversized shim to be rejected")
	}
}

// Anything that is not a shim still gets the same clear answer.
func TestShimFileCorrupt(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())

	if err := os.MkdirAll(toolsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shimPath("ni"), []byte("not a shim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readShimFile("ni"); err == nil {
		t.Fatal("expected a corrupt shim to be rejected")
	}
}
