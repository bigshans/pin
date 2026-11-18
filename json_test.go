package main

import (
	"encoding/json"
	"io"
	"math/rand"
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

// ---------- against encoding/json ----------

// encoding/json is the oracle for the reader that replaced it. Two directions,
// because they fail differently: too lax means pin swallows a file it should
// have refused, too strict means pin refuses a package.json that npm wrote.
//
// The oracle judges syntax only, hence RawMessage: pin never converts a number,
// so 1e1000 is a number here and an overflow there.
func FuzzReadPkgMeta(f *testing.F) {
	for _, seed := range []string{
		`{"a":"b","c":["d"],"e":1.5e3,"f":{"g":null},"bin":"x"}`,
		`{"version":"1.0.0","bin":{"a":"a.js","b":"b.js"}}`,
		`{"":"00"}`,
		`{"a":1e1000}`,
		`"\ud83d\ude00"`,
		`{"x":[1,2,{"y":[true,false,null]}]}`,
		`{"bin":null,"version":[]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		ok := jsonFirstValue(b)
		_, err := readPkgMeta(b)
		if err == nil && !ok {
			t.Fatalf("accepted what encoding/json rejects: %q", b)
		}
		// A package.json is always an object; anything else is not pin's input.
		if jsonFirstByte(b) == '{' && ok && err != nil {
			t.Fatalf("rejected an object encoding/json accepts: %v: %q", err, b)
		}
	})
}

// The same two directions without a fuzzing run: a fixed seed, short inputs,
// every byte of the alphabet a broken file could hold. 200k cases in ~0.1s.
func TestJSONRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []byte(`{}[]",:\/ 0123456789.eE+-truefalsnul\"u` + "\x00\t\n")
	for i := 0; i < 200000; i++ {
		b := make([]byte, rng.Intn(40))
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		ok := jsonFirstValue(b)
		_, err := readPkgMeta(b)
		if err == nil && !ok {
			t.Fatalf("accepted what encoding/json rejects: %q", b)
		}
		if jsonFirstByte(b) == '{' && ok && err != nil {
			t.Fatalf("rejected %v: %q", err, b)
		}
	}
}

// Escapes are where a hand-written reader goes wrong quietly: a lone surrogate,
// an escaped NUL, a valid pair. Every case is checked against encoding/json in
// both directions, including the writer's output coming back unchanged.
func TestJSONStrings(t *testing.T) {
	cases := []string{
		`"plain"`, `""`,
		`"a\u0041b"`, `"\u0000\u001f"`,
		`"\ud83d\ude00"`, `"\ud83d"`, `"\ude00"`, `"\ud83d\ud83d"`,
		`"\\\"\/\b\f\n\r\t"`,
		`"caf\u00e9"`, `"日本語"`, `"emoji-🚀"`,
	}
	for _, c := range cases {
		var want string
		if err := json.Unmarshal([]byte(c), &want); err != nil {
			t.Fatalf("%s: encoding/json rejects it: %v", c, err)
		}
		r := &jreader{b: []byte(c)}
		got, err := r.str()
		if err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		if got != want {
			t.Fatalf("%s: read %q, encoding/json says %q", c, got, want)
		}
		quoted := appendJSONString(nil, got)
		var back string
		if err := json.Unmarshal(quoted, &back); err != nil {
			t.Fatalf("writer emitted %q for %q: %v", quoted, got, err)
		}
		if back != want {
			t.Fatalf("%q → %q → %q", want, quoted, back)
		}
	}
}

// The syntax of a number is the whole of the reader's interest in it. 00 is the
// one a loose "read digits" loop gets wrong, and the one that made the fuzzer
// complain.
func TestJSONNumbers(t *testing.T) {
	good := []string{"0", "-0", "1", "-1", "1.5", "1e3", "1E+3", "1e-3", "-0.5e+3", "1e1000"}
	bad := []string{"00", "01", "-01", "1.", ".5", "+1", "-", "1e", "1e+", "0x1", "1 2"}
	for _, n := range good {
		if _, err := readPkgMeta([]byte(`{"a":` + n + `}`)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	for _, n := range bad {
		if _, err := readPkgMeta([]byte(`{"a":` + n + `}`)); err == nil {
			t.Errorf("%s was accepted", n)
		}
	}
}

// An empty array has to empty the slice it is read into: the registry is parsed
// entry by entry, and a reused Entry would otherwise keep the old bin names.
func TestJSONEmptyArrayResets(t *testing.T) {
	dst := []string{"stale"}
	r := &jreader{b: []byte(`{"bins":[]}`)}
	if err := r.object(func(k string) error { return r.intoStrings(&dst) }); err != nil {
		t.Fatal(err)
	}
	if len(dst) != 0 {
		t.Fatalf("bins kept %q", dst)
	}
}

// jsonFirstValue reports whether the head of b is one JSON value: trailing data
// is not pin's business, it only ever reads the start of a package.json.
func jsonFirstValue(b []byte) bool {
	var v json.RawMessage
	r := bytesReader(b)
	return json.NewDecoder(&r).Decode(&v) == nil
}

func jsonFirstByte(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return c
	}
	return 0
}

type bytesReader []byte

func (r *bytesReader) Read(p []byte) (int, error) {
	if len(*r) == 0 {
		return 0, io.EOF
	}
	n := copy(p, *r)
	*r = (*r)[n:]
	return n, nil
}
