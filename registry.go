package main

// The two JSON files pin touches: its own registry and a package's package.json.
// Both are read and written with the reader/writer in json.go.

import (
	"errors"
	"os"
	"sort"
)

func loadDB() (map[string]Entry, error) {
	b, err := os.ReadFile(dbPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]Entry{}
	r := &jreader{b: b}
	err = r.object(func(key string) error {
		var e Entry
		if err := r.entry(&e); err != nil {
			return err
		}
		m[key] = e
		return nil
	})
	if err != nil {
		return nil, err
	}
	r.skipWS()
	if r.i != len(b) {
		return nil, r.errf("trailing data after the registry")
	}
	return m, nil
}

func (r *jreader) entry(e *Entry) error {
	return r.object(func(k string) error {
		switch k {
		case "spec":
			return r.intoString(&e.Spec)
		case "version":
			return r.intoString(&e.Version)
		case "runtime":
			return r.intoString(&e.Runtime)
		case "nodePath":
			return r.intoString(&e.NodePath)
		case "pm":
			return r.intoString(&e.PM)
		case "bins":
			return r.intoStrings(&e.Bins)
		}
		return r.skipValue()
	})
}

// saveDB writes the registry. Keys are sorted so the file is stable across runs;
// the exact layout is not significant, only that it parses.
func saveDB(m map[string]Entry) error {
	if err := os.MkdirAll(pinHome(), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b := make([]byte, 0, 128+len(keys)*256)
	b = append(b, '{')
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		e := m[k]
		b = append(b, '\n', ' ', ' ')
		b = appendJSONString(b, k)
		b = append(b, ':', ' ', '{')
		b = appendJSONField(b, "spec", e.Spec)
		b = append(b, ',', '\n', ' ', ' ', ' ')
		b = appendJSONField(b, "version", e.Version)
		b = append(b, ',', '\n', ' ', ' ', ' ')
		b = appendJSONField(b, "runtime", e.Runtime)
		b = append(b, ',', '\n', ' ', ' ', ' ')
		b = appendJSONField(b, "nodePath", e.NodePath)
		b = append(b, ',', '\n', ' ', ' ', ' ')
		b = appendJSONString(b, "bins")
		b = append(b, ':', ' ', '[')
		for j, bin := range e.Bins {
			if j > 0 {
				b = append(b, ',', ' ')
			}
			b = appendJSONString(b, bin)
		}
		b = append(b, ']')
		if e.PM != "" {
			b = append(b, ',', '\n', ' ', ' ', ' ')
			b = appendJSONField(b, "pm", e.PM)
		}
		b = append(b, '\n', ' ', ' ', '}')
	}
	if len(keys) > 0 {
		b = append(b, '\n')
	}
	b = append(b, '}', '\n')
	return writeFileAtomic(dbPath(), b, 0o644)
}

func appendJSONField(b []byte, name, value string) []byte {
	b = appendJSONString(b, name)
	b = append(b, ':', ' ')
	return appendJSONString(b, value)
}

// pkgMeta is the part of a package.json pin cares about: which version landed,
// and where its executables are.
type pkgMeta struct {
	Version string
	BinOne  string            // "bin": "cli.js"
	BinMap  map[string]string // "bin": {"a": "a.js", "b": "b.js"}
}

func readPkgMeta(pj []byte) (pkgMeta, error) {
	var m pkgMeta
	r := &jreader{b: pj}
	err := r.object(func(k string) error {
		switch k {
		case "version":
			return r.intoString(&m.Version)
		case "bin":
			return r.intoBins(&m)
		}
		return r.skipValue()
	})
	return m, err
}

func (r *jreader) intoBins(m *pkgMeta) error {
	switch {
	case r.peek('"'):
		return r.intoString(&m.BinOne)
	case r.peek('{'):
		out := map[string]string{}
		err := r.object(func(k string) error {
			var v string
			if err := r.intoString(&v); err != nil {
				return err
			}
			if v != "" {
				out[k] = v
			}
			return nil
		})
		m.BinMap = out
		return err
	}
	return r.skipValue()
}

// bins flattens the two shapes npm allows into bin name → path. A package with
// no bin at all yields an empty map, which the caller reports.
func (m pkgMeta) bins(pkgName string) map[string]string {
	out := map[string]string{}
	if len(m.BinMap) > 0 {
		for k, v := range m.BinMap {
			out[k] = v
		}
		return out
	}
	if m.BinOne != "" {
		out[toolKey(pkgName)] = m.BinOne
	}
	return out
}
