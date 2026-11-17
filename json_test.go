package main

import (
	"os"
	"strings"
	"testing"
)

// The registry is the only thing pin writes that a human may look at, and it is
// re-read on every command, so both directions have to survive whatever a path
// or a package name contains.
func TestRegistryRoundTrip(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())

	nasty := "C:\\Users\\aerian\\.pin\\pkgs\\ni\\bin\\ni.mjs"
	cases := []Entry{
		{Spec: "cowsay@1.6.0", Version: "1.6.0", Runtime: "20.20.2", NodePath: "/n/node", Bins: []string{"cowsay", "cowthink"}, PM: "npm"},
		{Spec: "@antfu/ni", Version: "30.6.0", Runtime: "20.20.2", NodePath: nasty, Bins: []string{"ni"}},
		{Spec: "q", Version: "0.0.1", Runtime: "18", NodePath: "/n/node", Bins: []string{"quote\"d", "back\\slash", "tab\there", "line\nbreak", "ctrl\x01char"}},
		{Spec: "u", Version: "1.0.0", Runtime: "18", NodePath: "/n/node", Bins: []string{"中文", "emoji-🚀"}},
		{Spec: "empty", Version: "1.0.0", Runtime: "18", NodePath: "/n/node"},
	}
	db := map[string]Entry{}
	for i, e := range cases {
		db[cases[i].Version+"-"+string(rune('a'+i))] = e
	}
	if err := saveDB(db); err != nil {
		t.Fatal(err)
	}
	got, err := loadDB()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(db) {
		t.Fatalf("got %d entries, want %d", len(got), len(db))
	}
	for k, want := range db {
		g, ok := got[k]
		if !ok {
			t.Fatalf("%s went missing", k)
		}
		if len(g.Bins) != len(want.Bins) {
			t.Fatalf("%s: bins %q, want %q", k, g.Bins, want.Bins)
		}
		for i := range want.Bins {
			if g.Bins[i] != want.Bins[i] {
				t.Fatalf("%s: bin %d = %q, want %q", k, i, g.Bins[i], want.Bins[i])
			}
		}
		if g.Spec != want.Spec || g.Version != want.Version || g.Runtime != want.Runtime ||
			g.NodePath != want.NodePath || g.PM != want.PM {
			t.Fatalf("%s: got %+v, want %+v", k, g, want)
		}
	}
}

// A registry written by an earlier release went through encoding/json with the
// same field names; it has to load unchanged.
func TestRegistryReadsOldFormat(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())
	old := `{
  "cowsay": {
    "spec": "cowsay@1.6.0",
    "version": "1.6.0",
    "runtime": "20.20.2",
    "nodePath": "/home/u/.pin/../node/bin/node",
    "bins": [
      "cowsay",
      "cowthink"
    ],
    "pm": "pnpm"
  },
  "ni": {
    "spec": "@antfu/ni",
    "version": "30.6.0",
    "runtime": "20.20.2",
    "nodePath": "/n/node",
    "bins": ["ni"],
    "unknownField": {"nested": [1, 2.5e3, true, null, "x"]}
  }
}
`
	if err := os.MkdirAll(pinHome(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := loadDB()
	if err != nil {
		t.Fatal(err)
	}
	if db["cowsay"].PM != "pnpm" || len(db["cowsay"].Bins) != 2 || db["cowsay"].NodePath != "/home/u/.pin/../node/bin/node" {
		t.Fatalf("cowsay: %+v", db["cowsay"])
	}
	if db["ni"].Version != "30.6.0" || len(db["ni"].Bins) != 1 {
		t.Fatalf("ni: %+v", db["ni"])
	}
}

func TestRegistryRejectsGarbage(t *testing.T) {
	t.Setenv("PIN_HOME", t.TempDir())
	if err := os.MkdirAll(pinHome(), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		`{"a": {"spec": "x"`,
		`{"a" {"spec": "x"}}`,
		`{"a": {"spec": "x"}} trailing`,
		`{"a": {"spec": "x"}}}`,
		`{"a": {"spec": "\q"}}`,
		`{"a": {"spec": "raw
newline"}}`,
		`{"a": {"spec": "\u12"}}`,
	}
	for _, body := range bad {
		if err := os.WriteFile(dbPath(), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDB(); err == nil {
			t.Fatalf("accepted invalid registry: %q", body)
		}
	}
}

func TestPkgMetaBin(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    map[string]string
		wantErr bool
	}{
		{"string bin", `{"name":"cowsay","version":"1.6.0","bin":"cli.js"}`, map[string]string{"cowsay": "cli.js"}, false},
		{"object bin", `{"version":"2.0.0","bin":{"ni":"bin/ni.mjs","nr":"bin/nr.mjs"}}`, map[string]string{"ni": "bin/ni.mjs", "nr": "bin/nr.mjs"}, false},
		{"no bin", `{"version":"1.0.0"}`, map[string]string{}, false},
		{"null bin", `{"version":"1.0.0","bin":null}`, map[string]string{}, false},
		{"array bin", `{"version":"1.0.0","bin":["a.js"]}`, map[string]string{}, false},
		{"nested bin is not the top-level bin", `{"version":"1.0.0","scripts":{"bin":{"x":"y"}},"bin":{"real":"r.js"}}`, map[string]string{"real": "r.js"}, false},
		{"nested version is not the top-level version", `{"version":"1.0.0","exports":{"version":"9.9.9"}}`, map[string]string{}, false},
		{"escapes in paths", `{"version":"1.0.0","bin":{"a":"dir\/with\u0020space\n.js"}}`, map[string]string{"a": "dir/with space\n.js"}, false},
		{"surrogate pair", `{"version":"1.0.0","bin":{"x":"\ud83d\ude80.js"}}`, map[string]string{"x": "🚀.js"}, false},
		{"empty bin value dropped", `{"version":"1.0.0","bin":{"a":"","b":"b.js"}}`, map[string]string{"b": "b.js"}, false},
		{"malformed", `{"version":"1.0.0","bin":`, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta, err := readPkgMeta([]byte(c.body))
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := meta.bins("cowsay")
			if len(got) != len(c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Fatalf("got %q, want %q", got, c.want)
				}
			}
		})
	}
}

// A real package.json is mostly stuff pin does not read: dependencies, scripts
// with braces and quotes, huge nested fields. The scanner has to skip all of it
// and still find the two members it wants.
func TestPkgMetaSkipsLargeFields(t *testing.T) {
	big := strings.Repeat(`"x": [1, 2, {"y": "a}b]"}], `, 500)
	body := `{"name":"big","description":"has \"quotes\" and {braces} and [brackets]","keywords":["a","b"],
` + big + `"files":{"a":{"b":{"c":[[[[]]]]}}},
"version": "3.1.4", "dependencies": {"a": "^1.0.0", "b": "~2"}, "bin": {"big": "cli.js"}}`
	meta, err := readPkgMeta([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Version != "3.1.4" {
		t.Fatalf("version = %q", meta.Version)
	}
	if bins := meta.bins("big"); bins["big"] != "cli.js" {
		t.Fatalf("bins = %q", bins)
	}
}
