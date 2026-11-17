package main

import (
	"os"
	"path/filepath"
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

func TestKeyHolder(t *testing.T) {
	db := map[string]Entry{
		"cli":       {Spec: "@a/cli@1.0.0"},
		"ni":        {Spec: "@antfu/ni"},
		"other-key": {Spec: "plain"},
	}
	cases := []struct {
		name   string
		seen   map[string]string
		held   string
		clash  bool
		reason string
	}{
		{"@b/cli", nil, "@a/cli", true, "another scope, same last segment"},
		{"cli", nil, "@a/cli", true, "unscoped collides with a scope"},
		{"@a/cli", nil, "", false, "same package again: an upgrade"},
		{"@antfu/ni", nil, "", false, "same package again: an upgrade"},
		{"brand/new", nil, "", false, "free key"},
		{"plain", map[string]string{"other-key": "plain"}, "", false, "same package twice in one command"},
		{"@x/other-key", map[string]string{"other-key": "@y/other-key"}, "@y/other-key", true, "two scopes in one command"},
	}
	for _, c := range cases {
		held, clash := keyHolder(db, c.seen, c.name)
		if clash != c.clash || held != c.held {
			t.Errorf("%s: got (%q, %v), want (%q, %v) — %s", c.name, held, clash, c.held, c.clash, c.reason)
		}
	}
}

// An interrupted write must never leave a truncated registry behind: the shim
// metadata and the registry are the only record of what is pinned.
func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIN_HOME", dir)

	db := map[string]Entry{"ni": {Spec: "@antfu/ni", Bins: []string{"ni", "nr"}}}
	if err := saveDB(db); err != nil {
		t.Fatal(err)
	}
	db["cowsay"] = Entry{Spec: "cowsay", Bins: []string{"cowsay"}}
	if err := saveDB(db); err != nil {
		t.Fatal(err)
	}

	got, err := loadDB()
	if err != nil {
		t.Fatalf("registry unreadable after two saves: %v", err)
	}
	if len(got) != 2 || got["cowsay"].Spec != "cowsay" {
		t.Fatalf("unexpected registry contents: %+v", got)
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// A write that fails must leave the previous file intact rather than gutting it.
func TestWriteFileAtomicKeepsOldOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pins.json")
	if err := writeFileAtomic(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Renaming onto a path inside a directory that does not exist fails after
	// the data has been written, which is the interesting half of the sequence.
	if err := writeFileAtomic(filepath.Join(dir, "missing", "x"), []byte("new"), 0o644); err == nil {
		t.Fatal("expected the write to fail")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "old" {
		t.Fatalf("previous file damaged: %q, %v", b, err)
	}
}

func TestPMArgs(t *testing.T) {
	for _, pm := range []string{"npm", "pnpm", "yarn"} {
		args, ok := pmArgs(pm, "cowsay@1.6.0")
		if !ok || args[0] == "" || !strings.Contains(strings.Join(args, " "), "cowsay@1.6.0") {
			t.Errorf("%s: unexpected argv %q (ok=%v)", pm, args, ok)
		}
	}
	if _, ok := pmArgs("bun", "x"); ok {
		t.Error("bun should not be accepted")
	}
}

func TestChoosePM(t *testing.T) {
	cases := []struct {
		flag, env, want string
	}{
		{"pnpm", "", "pnpm"},     // the flag wins
		{"yarn", "pnpm", "yarn"}, // and beats the environment
		{"", "pnpm", "pnpm"},     // the environment is the persisted setting
		{"", "", "npm"},          // default
		{"", "bogus", "npm"},     // an unusable PIN_PM falls back
	}
	for _, c := range cases {
		t.Setenv("PIN_PM", c.env)
		if got := choosePM(c.flag); got != c.want {
			t.Errorf("flag=%q env=%q: got %q, want %q", c.flag, c.env, got, c.want)
		}
	}
}

func TestProjectName(t *testing.T) {
	for in, want := range map[string]string{
		"ni":      "ni",
		"Cowsay":  "cowsay",
		"@Scope!": "-scope-",
		"":        "pkg",
	} {
		if got := projectName(in); got != want {
			t.Errorf("projectName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Re-pointing an existing pin must rewrite the metadata and relink, without
// touching node_modules: the entry file does not change, only the interpreter.
func TestRebind(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())

	const oldNode, newNode = `C:\old\v20\node.exe`, `C:\new\v22\node.exe`
	if err := writeShimFile("nr", "node", oldNode, `C:\e\nr.mjs`, "ni", "20.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := makeShim("nr"); err != nil {
		t.Fatal(err)
	}
	// A shim that does not run node must be left alone.
	if err := writeShimFile("nf", "direct", "", `C:\e\nf.exe`, "ni", ""); err != nil {
		t.Fatal(err)
	}
	if err := makeShim("nf"); err != nil {
		t.Fatal(err)
	}

	db := map[string]Entry{"ni": {
		Spec: "@antfu/ni", Runtime: "20.0.0", NodePath: oldNode, Bins: []string{"nr", "nf"}, PM: "npm",
	}}
	if err := rebind("ni", db["ni"], newNode, "22.0.0", db); err != nil {
		t.Fatal(err)
	}

	sh, err := readShimFile("nr")
	if err != nil {
		t.Fatal(err)
	}
	if sh.Node != newNode || sh.Version != "22.0.0" {
		t.Errorf("node shim not rebound: %+v", *sh)
	}
	if sh, err := readShimFile("nf"); err != nil || sh.Node != "" {
		t.Errorf("non-node shim was touched: %+v %v", sh, err)
	}

	saved, err := loadDB()
	if err != nil {
		t.Fatal(err)
	}
	if saved["ni"].NodePath != newNode || saved["ni"].Runtime != "22.0.0" {
		t.Errorf("registry not updated: %+v", saved["ni"])
	}
	if len(saved["ni"].Bins) != 2 {
		t.Errorf("bins changed: %v", saved["ni"].Bins)
	}
}

// fakeRuntime lays out a directory findNode accepts, so runtime resolution works
// without touching the machine's real node installs.
func fakeRuntime(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, nodeExeName()), []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// `pin <pkg>` installs. On an already pinned package it must refuse and point at
// the commands that do the other two things.
func TestAddRefusesAlreadyPinned(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())
	dir := fakeRuntime(t)
	if err := saveDB(map[string]Entry{"cowsay": {
		Spec: "cowsay", Version: "1.6.0", Runtime: "20.20.2",
		NodePath: filepath.Join(dir, nodeExeName()), Bins: []string{"cowsay"}, PM: "npm",
	}}); err != nil {
		t.Fatal(err)
	}
	if code := cli([]string{"-n", dir, "cowsay"}); code != 1 {
		t.Fatalf("expected refusal, got %d", code)
	}
}

// A different runtime is the one case where add is allowed to touch an existing
// pin: it becomes a re-point.
func TestAddRepointsWithExplicitRuntime(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())
	oldDir := fakeRuntime(t)
	newDir := fakeRuntime(t)
	oldNode := filepath.Join(oldDir, nodeExeName())
	newNode := filepath.Join(newDir, nodeExeName())
	if err := writeShimFile("cowsay", "node", oldNode, filepath.Join(t.TempDir(), "e.js"), "cowsay", "1.6.0"); err != nil {
		t.Fatal(err)
	}
	if err := makeShim("cowsay"); err != nil {
		t.Fatal(err)
	}
	if err := saveDB(map[string]Entry{"cowsay": {
		Spec: "cowsay", Version: "1.6.0", Runtime: "20.20.2",
		NodePath: oldNode, Bins: []string{"cowsay"}, PM: "npm",
	}}); err != nil {
		t.Fatal(err)
	}
	if code := cli([]string{"-n", newDir, "cowsay"}); code != 0 {
		t.Fatalf("re-point failed with %d", code)
	}
	sh, err := readShimFile("cowsay")
	if err != nil {
		t.Fatal(err)
	}
	if sh.Node != newNode {
		t.Errorf("shim still points at %s, want %s", sh.Node, newNode)
	}
	if saved, _ := loadDB(); saved["cowsay"].NodePath != newNode {
		t.Errorf("registry not updated: %+v", saved["cowsay"])
	}
}

func TestUpdateRejectsUnknownPackage(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())
	if err := saveDB(map[string]Entry{}); err != nil {
		t.Fatal(err)
	}
	if code := cli([]string{"up", "nope"}); code != 1 {
		t.Fatalf("expected 1 for an unknown package, got %d", code)
	}
}

// A bin name is not a package name: `pin cowthink` must be understood as part of
// cowsay, not sent to the registry as a package that does not exist.
func TestAddRefusesBinName(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())
	dir := fakeRuntime(t)
	if err := saveDB(map[string]Entry{"cowsay": {
		Spec: "cowsay", Version: "1.6.0", Runtime: "20.20.2",
		NodePath: filepath.Join(dir, nodeExeName()), Bins: []string{"cowsay", "cowthink"}, PM: "npm",
	}}); err != nil {
		t.Fatal(err)
	}
	if code := cli([]string{"-n", dir, "cowthink"}); code != 1 {
		t.Fatalf("expected refusal for a bin name, got %d", code)
	}
}

func TestPinByName(t *testing.T) {
	db := map[string]Entry{
		"cowsay": {Spec: "cowsay", Bins: []string{"cowsay", "cowthink"}},
		"ni":     {Spec: "@antfu/ni", Bins: []string{"ni", "nr"}},
	}
	for _, c := range []struct {
		name string
		want string
	}{
		{"cowsay", "cowsay"},   // by key
		{"cowthink", "cowsay"}, // by bin
		{"nr", "ni"},           // by bin
		{"absent", ""},         // neither
	} {
		e, ok := pinByName(db, c.name)
		if c.want == "" {
			if ok {
				t.Errorf("%s: expected no match, got %+v", c.name, e)
			}
			continue
		}
		if !ok || e.Spec != db[c.want].Spec {
			t.Errorf("%s: got %+v (ok=%v), want %s", c.name, e, ok, c.want)
		}
	}
}
