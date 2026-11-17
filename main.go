// pin — pin globally installed CLI tools to a specific Node.js runtime.
//
// Layout (PIN_HOME, default ~/.pin):
//
//	bin/<tool>          hard link (copy on failure) of this executable
//	tools/<tool>.txt    per-shim metadata, 5 plain lines, no JSON at startup
//	pkgs/<name>/        npm --prefix target
//	pins.json           registry, only read by the CLI (never by a shim)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// version is overridden at build time with -X main.version.
var version = "0.1.0"

const shimMagic = "pin1"

type Entry struct {
	Spec     string   `json:"spec"`
	Version  string   `json:"version"`
	Runtime  string   `json:"runtime"`
	NodePath string   `json:"nodePath"`
	Bins     []string `json:"bins"`
	// PM is the package manager that materialised the directory. Empty means
	// npm, which is what every entry written before this field existed used.
	PM string `json:"pm,omitempty"`
}

// ---------- paths ----------

func pinHome() string {
	if h := os.Getenv("PIN_HOME"); h != "" {
		return h
	}
	hd, err := os.UserHomeDir()
	if err != nil {
		hd = "."
	}
	return filepath.Join(hd, ".pin")
}

func dbPath() string   { return filepath.Join(pinHome(), "pins.json") }
func binDir() string   { return filepath.Join(pinHome(), "bin") }
func toolsDir() string { return filepath.Join(pinHome(), "tools") }
func pkgDir(k string) string {
	return filepath.Join(pinHome(), "pkgs", k)
}

// ---------- registry ----------

func loadDB() (map[string]Entry, error) {
	b, err := os.ReadFile(dbPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]Entry{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveDB(m map[string]Entry) error {
	if err := os.MkdirAll(pinHome(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(dbPath(), b, 0o644)
}

// writeFileAtomic replaces path with data so that an interrupted write leaves
// either the old file or the new one, never a truncated one. This file is the
// only record of what is pinned: losing it strands every shim on disk. Rename
// is an atomic replace on POSIX and on Windows (MoveFileEx with REPLACE_EXISTING),
// which is the part of a real database we would actually miss.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once the rename succeeded
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------- shim metadata ----------

type shimInfo struct {
	Kind    string // node | direct | cmd
	Node    string
	Entry   string
	Tool    string
	Version string // runtime version label ("20.20.2"); lets a shim heal itself
}

func shimPath(bin string) string {
	return filepath.Join(toolsDir(), bin+".txt")
}

func writeShimFile(bin, kind, node, entry, tool, ver string) error {
	if err := os.MkdirAll(toolsDir(), 0o755); err != nil {
		return err
	}
	body := strings.Join([]string{shimMagic, kind, node, entry, tool, ver}, "\n") + "\n"
	return writeFileAtomic(shimPath(bin), []byte(body), 0o644)
}

// maxShimFile bounds what the hot path is willing to read. A shim is six short
// lines (200 bytes for @antfu/ni), but it lives on disk and can become anything:
// a stray redirection, a sync client gone wrong, a field added by a future
// version. Reading it whole into memory is fine at 200 bytes and not fine at
// 3MB, and this runs before every single invocation of a pinned tool.
const maxShimFile = 4 << 10

func readShimFile(bin string) (*shimInfo, error) {
	f, err := os.Open(shimPath(bin))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxShimFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxShimFile {
		return nil, fmt.Errorf("shim metadata for %q is over %d bytes; run 'pin doctor'", bin, maxShimFile)
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) < 5 || lines[0] != shimMagic {
		return nil, fmt.Errorf("corrupt shim for %q", bin)
	}
	info := &shimInfo{Kind: lines[1], Node: lines[2], Entry: lines[3], Tool: lines[4]}
	if len(lines) > 5 {
		info.Version = strings.TrimSpace(lines[5])
	}
	return info, nil
}

// ---------- entry point ----------

func main() {
	base := filepath.Base(os.Args[0])
	name := strings.TrimSuffix(base, ".exe")
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, ".EXE")
	}
	if isCLIName(name) {
		os.Exit(cli(os.Args[1:]))
	}
	os.Exit(runShim(name, os.Args[1:]))
}

// isCLIName reports whether argv[0] means "the CLI" rather than "a shim".
// A shim is the same binary under the pinned bin's name, so the name alone is
// not enough: metadata decides first, then the pin* names fall back to being
// the CLI (release artifacts such as pin-linux-amd64, dev builds such as
// pinchk.exe).
func isCLIName(name string) bool {
	if strings.EqualFold(name, "pin") {
		return true
	}
	// A real shim always has metadata, so a pinned tool called pin-github-action
	// or pinst wins over the name rule below.
	if _, err := os.Stat(shimPath(name)); err == nil {
		return false
	}
	// No metadata: a name starting with "pin" is a release artifact
	// (pin-linux-amd64) or a dev build (pinchk.exe), never an installed tool.
	return strings.HasPrefix(strings.ToLower(name), "pin")
}

// runShim is the hot path: one small file read, then exec.
func runShim(name string, args []string) int {
	sh, err := readShimFile(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pin: %s: %v\n  run 'pin doctor' to rebuild shims\n", name, err)
		return 127
	}

	var argv []string
	env := os.Environ()

	switch sh.Kind {
	case "node":
		if _, err := os.Stat(sh.Node); err != nil && sh.Version != "" {
			// The runtime vanished (manager upgrade / uninstall). Try to find the
			// same version elsewhere and rewrite the shim, so users do not have
			// to reinstall just because a path moved.
			if _, np, ok := findRuntimeByVersion(sh.Version); ok {
				sh.Node = np
				_ = writeShimFile(name, sh.Kind, np, sh.Entry, sh.Tool, sh.Version)
			}
		}
		if _, err := os.Stat(sh.Node); err != nil {
			fmt.Fprintf(os.Stderr, "pin: runtime for %s is gone (%s)\n  %s\n  or: pin %s -n <ver> --install\n", name, sh.Node, installHint(sh.Node, sh.Version), sh.Tool)
			return 127
		}
		if _, err := os.Stat(sh.Entry); err != nil {
			fmt.Fprintf(os.Stderr, "pin: entry for %s is gone (%s)\n  fix: pin rm %s && pin add ...\n", name, sh.Entry, sh.Tool)
			return 127
		}
		// The entry always runs on the pinned runtime (argv[0] is absolute).
		// Child processes are a different question: tools like ni/npm/pnpm only
		// forward to the project's package manager, so whatever the shell's
		// version manager put on PATH must win. Append as a fallback instead of
		// prepending, and let PIN_STRICT_RUNTIME=1 restore the old behaviour for
		// tools that must stay on their own npm/node.
		if os.Getenv("PIN_STRICT_RUNTIME") != "" {
			env = prependPath(env, filepath.Dir(sh.Node))
		} else {
			env = appendPath(env, filepath.Dir(sh.Node))
		}
		argv = append([]string{sh.Node, sh.Entry}, args...)
	case "cmd":
		if runtime.GOOS == "windows" {
			argv = append([]string{"cmd.exe", "/c", sh.Entry}, args...)
		} else {
			argv = append([]string{sh.Entry}, args...)
		}
	default: // direct
		argv = append([]string{sh.Entry}, args...)
	}
	return runArgv(argv, env)
}

func prependPath(env []string, dir string) []string {
	sep := string(os.PathListSeparator)
	for i, kv := range env {
		if len(kv) > 5 && strings.EqualFold(kv[:5], "PATH=") {
			env[i] = kv[:5] + dir + sep + kv[5:]
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// appendPath puts dir last on PATH: it only answers when nothing earlier in
// PATH provides that executable, so the project's own runtime keeps priority.
func appendPath(env []string, dir string) []string {
	sep := string(os.PathListSeparator)
	for i, kv := range env {
		if len(kv) > 5 && strings.EqualFold(kv[:5], "PATH=") {
			rest := kv[5:]
			if rest == "" {
				env[i] = kv[:5] + dir
			} else {
				env[i] = kv + sep + dir
			}
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// ---------- CLI ----------

func cli(args []string) int {
	if len(args) == 0 {
		return cmdList()
	}

	if !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "a", "add", "i", "install":
			return cmdAdd(args[1:])
		case "l", "ls", "list":
			return cmdList()
		case "r", "rm", "remove", "u", "uninstall":
			return cmdRemove(args[1:])
		case "w", "which":
			return cmdWhich(args[1:])
		case "d", "doctor":
			return cmdDoctor(args[1:])
		case "s", "shims":
			return cmdShims()
		case "completion":
			return cmdCompletion(args[1:])
		case "__complete":
			return cmdComplete(args[1:])
		case "h", "help":
			usage()
			return 0
		case "v", "version":
			printVersion()
			return 0
		}
		// No verb: `pin <pkg>...` is shorthand for `pin add <pkg>...`.
		return cmdAdd(args)
	}

	switch args[0] {
	case "-v", "--version":
		printVersion()
		return 0
	case "-h", "--help":
		usage()
		return 0
	}
	// Leading flags (e.g. `pin -n 20 pkg`) belong to add.
	return cmdAdd(args)
}

func printVersion() {
	fmt.Println("pin", version, runtime.GOOS+"/"+runtime.GOARCH)
}

func usage() {
	os.Stdout.WriteString(`pin ` + version + ` — pin global CLI tools to a Node runtime

usage:
  pin <pkg>[@ver]...       install & pin with the current node
  pin -n <ver|dir> <pkg>   same, with a chosen runtime  (pin -n 20 @antfu/ni)
  pin --pm <name> <pkg>    install with npm, pnpm or yarn (default: npm)
  pin --install -n <ver>   install the runtime first if it is missing
  pin                      list pinned tools        (= pin l)
  pin l                    list
  pin r <name|bin>...      unpin (bin names accepted)
  pin w <bin>              show what a shim resolves to
  pin d [--fix]            verify, and re-bind stale shims / drop orphans
  pin s                    relink shims (after upgrading pin)
  pin completion <shell>   print completion glue (bash|zsh|fish|powershell)
  pin v | pin -v           print version

verbs also accept full names: add / ls / rm / which / doctor / shims

env:
  PIN_HOME            install root (default ~/.pin)
  PIN_NODE            default runtime dir when -n is omitted
  PIN_PM              default package manager when --pm is omitted
  PIN_STRICT_RUNTIME  keep the pinned runtime first on PATH for child processes

put ~/.pin/bin (or %USERPROFILE%\.pin\bin) on PATH once, then never again.
`)
}

// splitSpec splits "@scope/name@1.2.3" / "name@1.2.3" / "name".
func splitSpec(spec string) (name, ver string) {
	if strings.HasPrefix(spec, "@") {
		if i := strings.Index(spec[1:], "@"); i >= 0 {
			return spec[:i+1], spec[i+2:]
		}
		return spec, ""
	}
	if i := strings.Index(spec, "@"); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

func toolKey(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// keyHolder reports which package already owns name's key: one already in the
// registry, or an earlier package in the same command. Both the install
// directory and the registry key are the last path segment, so @a/cli and
// @b/cli collide there even though npm treats them as different packages.
func keyHolder(db map[string]Entry, seen map[string]string, name string) (string, bool) {
	key := toolKey(name)
	if other, ok := seen[key]; ok && other != name {
		return other, true
	}
	if e, ok := db[key]; ok {
		if prev, _ := splitSpec(e.Spec); prev != name {
			return prev, true
		}
	}
	return "", false
}

// ---------- runtime resolution ----------

func findNode(dir string) (string, error) {
	cands := []string{
		filepath.Join(dir, nodeExeName()),
		filepath.Join(dir, "bin", nodeExeName()),
		filepath.Join(dir, "installation", nodeExeName()),
		filepath.Join(dir, "installation", "bin", nodeExeName()),
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("no %s under %s", nodeExeName(), dir)
}

const verToken = "@V@"

// runtimeManager describes one way node can be installed on this machine:
// where its versions live, and how to install another one. Everything else
// (lookup, self-healing, hints, --install) is driven by this table, so no
// single manager is hardcoded anywhere.
type runtimeManager struct {
	name    string
	dirs    []string // glob patterns, @V@ replaced by the version
	install []string // argv after the binary, @V@ replaced by the version
}

var runtimeManagers = []runtimeManager{
	{
		name: "fnm",
		dirs: []string{
			"$FNM_DIR/node-versions/@V@",
			"$HOME/scoop/apps/fnm/current/node-versions/@V@",
			"$HOME/.local/share/fnm/node-versions/@V@",
			"$HOME/.fnm/node-versions/@V@",
		},
		install: []string{"install", "@V@"},
	},
	{
		name: "nvm",
		dirs: []string{
			"$NVM_DIR/versions/node/@V@",
			"$HOME/.nvm/versions/node/@V@",
		},
		install: []string{"install", "@V@"},
	},
	{
		name: "mise",
		dirs: []string{
			"$MISE_DATA_DIR/installs/node/@V@",
			"$HOME/.local/share/mise/installs/node/@V@",
		},
		install: []string{"install", "node@@V@"},
	},
	{
		name: "asdf",
		dirs: []string{
			"$ASDF_DATA_DIR/installs/nodejs/@V@",
			"$HOME/.asdf/installs/nodejs/@V@",
		},
		install: []string{"install", "nodejs", "@V@"},
	},
	{
		name: "proto",
		dirs: []string{
			"$PROTO_HOME/tools/node/@V@",
			"$HOME/.proto/tools/node/@V@",
		},
		install: []string{"install", "node@@V@"},
	},
	{
		name: "vfox",
		dirs: []string{
			"$HOME/.vfox/cache/nodejs/v-@V@",
		},
		install: []string{"install", "nodejs@@V@"},
	},
	{
		name:    "volta",
		dirs:    []string{"$HOME/.volta/tools/image/node/@V@"},
		install: []string{"install", "node@@V@"},
	},
}

// expandVars expands $VAR and returns "" when any variable is unset, so the
// caller can skip that location entirely.
func expandVars(p string) string {
	missing := false
	out := os.Expand(p, func(k string) string {
		if k == "HOME" || k == "USERPROFILE" {
			h, err := os.UserHomeDir()
			if err != nil {
				missing = true
				return ""
			}
			return h
		}
		v := os.Getenv(k)
		if v == "" {
			missing = true
		}
		return v
	})
	if missing {
		return ""
	}
	return out
}

// versionVariants yields the bare and v-prefixed spellings of a version, since
// managers disagree ("v20.20.2" in fnm, "20.20.2" in mise/asdf).
func versionVariants(ver string) []string {
	if strings.HasPrefix(ver, "v") {
		return []string{ver, strings.TrimPrefix(ver, "v")}
	}
	return []string{ver, "v" + ver}
}

// managerForPath reports which manager owns a node path (best effort), used
// only to print the right fix command.
func managerForPath(nodePath string) *runtimeManager {
	for i := range runtimeManagers {
		m := &runtimeManagers[i]
		for _, d := range m.dirs {
			idx := strings.Index(d, verToken)
			if idx < 0 {
				continue
			}
			root := expandVars(d[:idx])
			if root == "" {
				continue
			}
			// Managers disagree on separators ($FNM_DIR may use "\\"), so
			// normalise both sides before comparing.
			n := strings.ToLower(filepath.ToSlash(filepath.Clean(nodePath)))
			r := strings.ToLower(filepath.ToSlash(filepath.Clean(root)))
			if strings.HasPrefix(n, r+"/") || strings.HasPrefix(n, r) {
				return m
			}
		}
	}
	return nil
}

// installHint renders "fnm install 20" (or whatever manager owns the path).
func installHint(nodePath, ver string) string {
	if ver == "" {
		ver = "<ver>"
	}
	if m := managerForPath(nodePath); m != nil {
		args := make([]string, len(m.install))
		for i, a := range m.install {
			args[i] = strings.ReplaceAll(a, verToken, ver)
		}
		return "fix: " + m.name + " " + strings.Join(args, " ")
	}
	return "fix: install node " + ver + " with your version manager"
}

// versionPatterns returns glob patterns across every known manager, so that a
// partial version like "20" matches v20.20.2 / 20.9.0.
func versionPatterns(ver string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range runtimeManagers {
		for _, dir := range m.dirs {
			for _, v := range versionVariants(ver) {
				p := expandVars(strings.ReplaceAll(dir, verToken, v+"*"))
				if p == "" || seen[p] {
					continue
				}
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// verLess compares two paths by the trailing numeric components of their
// basenames ("v20.9.0" < "v20.20.2"), so the newest match wins.
func verLess(a, b string) bool {
	na, nb := versionNumbers(a), versionNumbers(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			return na[i] < nb[i]
		}
	}
	return len(na) < len(nb)
}

func versionNumbers(p string) []int {
	base := filepath.Base(p)
	var nums []int
	cur := ""
	for _, r := range base {
		if r >= '0' && r <= '9' {
			cur += string(r)
			continue
		}
		if cur != "" {
			n, _ := strconv.Atoi(cur)
			nums = append(nums, n)
			cur = ""
		}
	}
	if cur != "" {
		n, _ := strconv.Atoi(cur)
		nums = append(nums, n)
	}
	return nums
}

func knownRuntimes() string {
	seen := map[string]bool{}
	var found []string
	for _, m := range runtimeManagers {
		for _, dir := range m.dirs {
			p := expandVars(strings.ReplaceAll(dir, verToken, "*"))
			if p == "" {
				continue
			}
			matches, _ := filepath.Glob(p)
			for _, x := range matches {
				v := runtimeLabel(x)
				if v == "" || v[0] < '0' || v[0] > '9' || seen[v] {
					continue
				}
				if true {
					seen[v] = true
					found = append(found, v)
				}
			}
		}
	}
	if len(found) == 0 {
		return "none installed"
	}
	sort.Strings(found)
	return strings.Join(found, ", ")
}

func findRuntimeByVersion(ver string) (string, string, bool) {
	var hits []string
	for _, pat := range versionPatterns(ver) {
		m, _ := filepath.Glob(pat)
		hits = append(hits, m...)
	}
	sort.Slice(hits, func(i, j int) bool { return verLess(hits[i], hits[j]) })
	for i := len(hits) - 1; i >= 0; i-- {
		if p, err := findNode(hits[i]); err == nil {
			return runtimeLabel(hits[i]), p, true
		}
	}
	return "", "", false
}

// installRuntime delegates installation to whichever manager is on PATH. pin
// itself never downloads anything: no index, no checksums, no mirrors.
func installRuntime(ver string) error {
	var present []string
	var failed []string
	for _, m := range runtimeManagers {
		if _, err := exec.LookPath(m.name); err != nil {
			continue
		}
		present = append(present, m.name)
		for _, v := range versionVariants(ver) {
			args := make([]string, len(m.install))
			for i, a := range m.install {
				args[i] = strings.ReplaceAll(a, verToken, v)
			}
			fmt.Println("-> " + m.name + " " + strings.Join(args, " "))
			c := exec.Command(m.name, args...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err == nil {
				return nil
			}
			failed = append(failed, m.name+" "+strings.Join(args, " "))
		}
	}
	if len(present) == 0 {
		return errors.New("no supported manager on PATH (fnm, nvm, mise, asdf, proto, vfox, volta)")
	}
	return errors.New("install failed: " + strings.Join(failed, "; "))
}

// runtimeLabel turns .../node-versions/v20.20.2/installation into "20.20.2".
func runtimeLabel(dir string) string {
	b := filepath.Base(dir)
	if b == "installation" || b == "bin" {
		b = filepath.Base(filepath.Dir(dir))
	}
	b = strings.TrimPrefix(b, "v")
	return strings.TrimPrefix(b, "-")
}

// resolveRuntime returns (runtimeLabel, nodePath).
func resolveRuntime(flag string) (string, string, error) {
	if flag != "" {
		if p, err := findNode(flag); err == nil {
			return runtimeLabel(flag), p, nil
		}
		if label, p, ok := findRuntimeByVersion(flag); ok {
			return label, p, nil
		}
		return "", "", fmt.Errorf("runtime %q not found (known: %s); install it with your manager or pass -n <dir>", flag, knownRuntimes())
	}
	if p := os.Getenv("PIN_NODE"); p != "" {
		if np, err := findNode(p); err == nil {
			return runtimeLabel(p), np, nil
		}
	}
	if lp, err := exec.LookPath(nodeExeName()); err == nil {
		if abs, err := filepath.Abs(lp); err == nil {
			d := filepath.Dir(abs)
			if real, err := filepath.EvalSymlinks(abs); err == nil {
				// fnm/volta shims live in a side dir; prefer the real install dir.
				if np, err := findNode(filepath.Dir(real)); err == nil {
					return filepath.Dir(real), np, nil
				}
			}
			if np, err := findNode(d); err == nil {
				return d, np, nil
			}
		}
	}
	return "", "", errors.New("no node runtime found; pass --runtime <ver|dir> or set PIN_NODE")
}

// pmArgs turns a package manager into the argv that materialises spec inside
// the current directory. All pin needs from a manager is "put the package in
// ./node_modules/<name>", which is why supporting one is a case here rather
// than a code path: the bin discovery reads package.json and never looks at the
// layout. npm and pnpm write two different tree shapes and it does not matter.
func pmArgs(pm, spec string) ([]string, bool) {
	switch pm {
	case "npm":
		return []string{"install", spec, "--no-fund", "--no-audit", "--loglevel=error"}, true
	case "pnpm":
		return []string{"add", spec, "--reporter=silent"}, true
	case "yarn":
		return []string{"add", spec}, true
	}
	return nil, false
}

// choosePM decides the package manager for one package: --pm wins, then what the
// package was installed with before (re-pinning is an upgrade, and silently
// switching layout on an upgrade is not what anyone means), then PIN_PM, then npm.
func choosePM(flag string, prev Entry) string {
	if flag != "" {
		return flag
	}
	if prev.PM != "" {
		return prev.PM
	}
	if env := os.Getenv("PIN_PM"); env != "" {
		if _, ok := pmArgs(env, ""); ok {
			return env
		}
	}
	return "npm"
}

func prevPM(e Entry) string {
	if e.PM == "" {
		return "npm"
	}
	return e.PM
}

// installPackage treats the install directory as a tiny private project and
// lets the chosen manager fill in node_modules. This is the whole trick: pin
// owns the destination, so it never has to understand how a manager would have
// picked one on its own.
func installPackage(pm, key, prefix, spec, nodePath string) error {
	args, ok := pmArgs(pm, spec)
	if !ok {
		return fmt.Errorf("unknown package manager %q", pm)
	}
	bin, err := exec.LookPath(pm)
	if err != nil {
		return fmt.Errorf("%s is not on PATH", pm)
	}
	if err := ensureProject(prefix, key); err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = prefix
	// The pinned runtime first, so postinstall scripts that need node get the
	// version this package is being pinned to.
	cmd.Env = prependPath(os.Environ(), filepath.Dir(nodePath))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// ensureProject gives the manager something to install into. It is deliberately
// minimal: no dependencies, no scripts, private so it can never be published.
func ensureProject(prefix, key string) error {
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return err
	}
	pj := filepath.Join(prefix, "package.json")
	if _, err := os.Stat(pj); err == nil {
		return nil
	}
	body := fmt.Sprintf("{\n  \"name\": \"pin-%s\",\n  \"version\": \"0.0.0\",\n  \"private\": true\n}\n", projectName(key))
	return writeFileAtomic(pj, []byte(body), 0o644)
}

// projectName keeps the synthetic package.json name legal: npm rejects
// uppercase and anything outside a small character set.
func projectName(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "pkg"
	}
	return b.String()
}

// ---------- commands ----------

func cmdAdd(args []string) int {
	var rtFlag, pmFlag string
	var specs []string
	withInstall := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--install":
			withInstall = true
		case a == "-n" || a == "-r" || a == "--node" || a == "--runtime":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "pin: "+a+" needs a value (e.g. -n 20)")
				return 2
			}
			i++
			rtFlag = args[i]
		case strings.HasPrefix(a, "-n=") || strings.HasPrefix(a, "-r=") ||
			strings.HasPrefix(a, "--node=") || strings.HasPrefix(a, "--runtime="):
			_, rtFlag, _ = strings.Cut(a, "=")
		case a == "--pm":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "pin: --pm needs a value (npm, pnpm or yarn)")
				return 2
			}
			i++
			pmFlag = args[i]
		case strings.HasPrefix(a, "--pm="):
			_, pmFlag, _ = strings.Cut(a, "=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "pin: unknown flag %s\n", a)
			return 2
		default:
			specs = append(specs, a)
		}
	}
	if len(specs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pin [-n <ver|dir>] [--pm npm|pnpm|yarn] <pkg>[@ver]...")
		return 2
	}
	if pmFlag != "" {
		if _, ok := pmArgs(pmFlag, ""); !ok {
			fmt.Fprintf(os.Stderr, "pin: unknown package manager %q (npm, pnpm or yarn)\n", pmFlag)
			return 2
		}
	}

	rtLabel, nodePath, err := resolveRuntime(rtFlag)
	if err != nil && withInstall && rtFlag != "" {
		if ierr := installRuntime(rtFlag); ierr != nil {
			fmt.Fprintln(os.Stderr, "pin:", ierr)
			return 1
		}
		rtLabel, nodePath, err = resolveRuntime(rtFlag)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}
	db, err := loadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}

	// Check every spec before touching the disk. The directory and the registry
	// key both come from the last path segment, so @a/cli and @b/cli would share
	// them: installing the second would overwrite the first's entry, and `pin r`
	// would then delete both sets of files under it.
	seen := map[string]string{}
	for _, spec := range specs {
		pkgName, _ := splitSpec(spec)
		if held, ok := keyHolder(db, seen, pkgName); ok {
			fmt.Fprintf(os.Stderr, "pin: %q is already taken by %s; pin r %s first, or pin a different package\n", toolKey(pkgName), held, toolKey(pkgName))
			return 1
		}
		seen[toolKey(pkgName)] = pkgName
	}

	for _, spec := range specs {
		pkgName, _ := splitSpec(spec)
		key := toolKey(pkgName)
		prefix := pkgDir(key)
		pm := choosePM(pmFlag, db[key])
		if prev, ok := db[key]; ok && prevPM(prev) != pm {
			// A flat npm tree and a pnpm store do not mix, and a lockfile from
			// one manager means nothing to another.
			fmt.Printf("   reinstalling with %s (was %s)\n", pm, prevPM(prev))
			if err := os.RemoveAll(prefix); err != nil {
				fmt.Fprintln(os.Stderr, "pin:", err)
				return 1
			}
		}

		fmt.Printf("→ %s  (runtime %s, via %s)\n", spec, rtLabel, pm)
		if err := installPackage(pm, key, prefix, spec, nodePath); err != nil {
			fmt.Fprintf(os.Stderr, "pin: %s install failed for %s: %v\n", pm, spec, err)
			if pm == "yarn" {
				fmt.Fprintln(os.Stderr, "  yarn 2+ defaults to Plug'n'Play, which has no node_modules; set nodeLinker: node-modules in .yarnrc.yml")
			}
			return 1
		}

		instDir := filepath.Join(prefix, "node_modules", filepath.FromSlash(pkgName))
		pj, err := os.ReadFile(filepath.Join(instDir, "package.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "pin: cannot read %s/package.json\n", instDir)
			return 1
		}
		var meta struct {
			Version string          `json:"version"`
			Bin     json.RawMessage `json:"bin"`
		}
		if err := json.Unmarshal(pj, &meta); err != nil {
			fmt.Fprintln(os.Stderr, "pin:", err)
			return 1
		}

		bins := parseBins(pkgName, meta.Bin)
		if len(bins) == 0 {
			fmt.Fprintf(os.Stderr, "pin: %s publishes no bin, nothing to pin\n", pkgName)
			return 1
		}

		names := make([]string, 0, len(bins))
		for bin, rel := range bins {
			// "pin" itself is the CLI binary; a shim of that name would shadow or
			// clobber it, and isCLIName would keep routing it to the CLI anyway.
			if bin == "pin" {
				fmt.Fprintf(os.Stderr, "pin: skip bin %q: it collides with the pin CLI\n", bin)
				continue
			}
			entry := filepath.Join(instDir, filepath.FromSlash(rel))
			kind := kindOf(entry)
			if _, err := os.Stat(entry); err != nil {
				fmt.Fprintf(os.Stderr, "pin: warn: %s entry missing (%s)\n", bin, entry)
			}
			if err := writeShimFile(bin, kind, nodePath, entry, key, rtLabel); err != nil {
				fmt.Fprintln(os.Stderr, "pin:", err)
				return 1
			}
			if err := makeShim(bin); err != nil {
				fmt.Fprintln(os.Stderr, "pin:", err)
				return 1
			}
			names = append(names, bin)
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Fprintf(os.Stderr, "pin: %s publishes no usable bin\n", pkgName)
			return 1
		}

		prev := db[key]
		for _, old := range prev.Bins {
			if !contains(names, old) {
				_ = os.Remove(shimPath(old))
				_ = os.Remove(filepath.Join(binDir(), old+exeSuffix()))
			}
		}

		db[key] = Entry{
			Spec:     spec,
			Version:  meta.Version,
			Runtime:  rtLabel,
			NodePath: nodePath,
			Bins:     names,
			PM:       pm,
		}
		fmt.Printf("  pinned %s@%s → %s\n", pkgName, meta.Version, strings.Join(names, ", "))
	}

	if err := saveDB(db); err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}
	fmt.Printf("PATH hint: %s\n", binDir())
	return 0
}

func parseBins(pkgName string, raw json.RawMessage) map[string]string {
	out := map[string]string{}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return out
	}
	if strings.HasPrefix(s, `"`) {
		var one string
		if json.Unmarshal(raw, &one) == nil && one != "" {
			out[toolKey(pkgName)] = one
		}
		return out
	}
	m := map[string]string{}
	if json.Unmarshal(raw, &m) == nil {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func kindOf(entry string) string {
	switch strings.ToLower(filepath.Ext(entry)) {
	case ".js", ".mjs", ".cjs":
		return "node"
	case ".cmd", ".bat", ".ps1":
		return "cmd"
	}
	return "direct"
}

func makeShim(bin string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binDir(), 0o755); err != nil {
		return err
	}
	dst := filepath.Join(binDir(), bin+exeSuffix())
	_ = os.Remove(dst)
	if err := os.Link(self, dst); err == nil {
		return nil
	}
	// Cross-device or FS without hard links.
	b, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

func cmdList() int {
	db, err := loadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}
	if len(db) == 0 {
		fmt.Println("no pinned tools")
		return 0
	}
	keys := make([]string, 0, len(db))
	w := 4
	for k := range db {
		keys = append(keys, k)
		if len(k) > w {
			w = len(k)
		}
	}
	sort.Strings(keys)
	fmt.Printf("%-*s  %-12s  %-10s  %-4s  %s\n", w, "NAME", "VERSION", "RUNTIME", "PM", "BINS")
	for _, k := range keys {
		e := db[k]
		fmt.Printf("%-*s  %-12s  %-10s  %-4s  %s\n", w, k, e.Version, shorten(e.Runtime, 10), prevPM(e), strings.Join(e.Bins, ", "))
	}
	return 0
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
}

func cmdRemove(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pin rm <name|bin>...")
		return 2
	}
	db, err := loadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}
	rc := 0
	for _, arg := range args {
		key := arg
		if _, ok := db[key]; !ok {
			for k, e := range db {
				if contains(e.Bins, arg) {
					key = k
					break
				}
			}
		}
		e, ok := db[key]
		if !ok {
			fmt.Fprintf(os.Stderr, "pin: %s is not pinned\n", arg)
			rc = 1
			continue
		}
		for _, b := range e.Bins {
			_ = os.Remove(shimPath(b))
			_ = os.Remove(filepath.Join(binDir(), b+exeSuffix()))
		}
		_ = os.RemoveAll(pkgDir(key))
		delete(db, key)
		fmt.Printf("removed %s (%s)\n", key, e.Version)
	}
	if err := saveDB(db); err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}
	return rc
}

func cmdWhich(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: pin which <bin>")
		return 2
	}
	sh, err := readShimFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "pin: %s: %v\n", args[0], err)
		return 1
	}
	fmt.Printf("tool:    %s\nkind:    %s\nruntime: %s\nentry:   %s\n", sh.Tool, sh.Kind, sh.Node, sh.Entry)
	return 0
}

func cmdDoctor(args []string) int {
	fix := false
	for _, a := range args {
		if a == "--fix" || a == "-f" {
			fix = true
		}
	}

	db, err := loadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}
	self, _ := os.Executable()
	selfSize := int64(-1)
	if st, err := os.Stat(self); err == nil {
		selfSize = st.Size()
	}

	bad, healed := 0, 0
	for key, e := range db {
		// Registry-level: runtime path gone?
		if _, err := os.Stat(e.NodePath); err != nil {
			if _, np, ok := findRuntimeByVersion(e.Runtime); ok {
				e.NodePath = np
				db[key] = e
				healed++
				fmt.Printf("~ %-12s runtime moved → %s\n", key, np)
			} else {
				fmt.Printf("✗ %-12s runtime gone: %s\n", key, e.NodePath)
				bad++
			}
		}

		for _, b := range e.Bins {
			sh, err := readShimFile(b)
			if err != nil {
				fmt.Printf("✗ %-12s shim %s: %v\n", key, b, err)
				bad++
				continue
			}
			if sh.Kind == "node" {
				if _, err := os.Stat(sh.Node); err != nil {
					if _, np, ok := findRuntimeByVersion(sh.Version); ok {
						if fix {
							sh.Node = np
							if err := writeShimFile(b, sh.Kind, np, sh.Entry, sh.Tool, sh.Version); err != nil {
								fmt.Printf("✗ %-12s shim %s: %v\n", key, b, err)
								bad++
								continue
							}
							healed++
							fmt.Printf("~ %-12s shim %s re-bound → %s\n", key, b, np)
						} else {
							fmt.Printf("! %-12s shim %s points at a stale runtime (run: pin d --fix)\n", key, b)
						}
					} else {
						fmt.Printf("✗ %-12s shim %s: runtime gone (%s)\n", key, b, sh.Node)
						bad++
					}
				}
			}
			if _, err := os.Stat(sh.Entry); err != nil {
				fmt.Printf("✗ %-12s entry gone: %s\n", key, sh.Entry)
				bad++
			}
			link := filepath.Join(binDir(), b+exeSuffix())
			if st, err := os.Stat(link); err != nil {
				if fix {
					if err := makeShim(b); err != nil {
						fmt.Printf("✗ %-12s %v\n", key, err)
						bad++
					} else {
						healed++
						fmt.Printf("~ %-12s relinked %s\n", key, b)
					}
				} else {
					fmt.Printf("✗ %-12s shim missing: %s (run: pin d --fix)\n", key, link)
					bad++
				}
			} else if selfSize >= 0 && st.Size() != selfSize {
				if fix {
					if err := makeShim(b); err == nil {
						healed++
						fmt.Printf("~ %-12s relinked stale shim %s\n", key, b)
					}
				} else {
					fmt.Printf("! %-12s shim is a stale copy: %s (run: pin d --fix)\n", key, link)
				}
			}
		}
	}
	if healed > 0 {
		_ = saveDB(db)
	}

	// Shims on disk that no registry entry owns.
	orphans := 0
	if ents, err := os.ReadDir(toolsDir()); err == nil {
		for _, de := range ents {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".txt") {
				continue
			}
			bin := strings.TrimSuffix(de.Name(), ".txt")
			sh, err := readShimFile(bin)
			if err != nil {
				continue
			}
			if _, ok := db[sh.Tool]; ok {
				continue
			}
			const mark = "✗ orphan shim: %s (tool %s not in registry)\n"
			if fix {
				_ = os.Remove(shimPath(bin))
				_ = os.Remove(filepath.Join(binDir(), bin+exeSuffix()))
				healed++
				fmt.Printf("~ removed orphan shim %s (tool %s)\n", bin, sh.Tool)
			} else {
				fmt.Printf(mark+"  fix: pin d --fix, or pin r %s\n", bin, sh.Tool, sh.Tool)
				orphans++
			}
		}
	}

	switch {
	case bad > 0:
		return 1
	case orphans > 0:
		return 1
	case healed > 0:
		fmt.Printf("fixed %d issue(s); %d pinned tool(s) healthy\n", healed, len(db))
		return 0
	default:
		fmt.Printf("ok: %d pinned tools healthy\n", len(db))
		return 0
	}
}

func cmdShims() int {
	ents, err := os.ReadDir(toolsDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pin:", err)
		return 1
	}
	n := 0
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".txt") {
			continue
		}
		bin := strings.TrimSuffix(de.Name(), ".txt")
		if err := makeShim(bin); err != nil {
			fmt.Fprintf(os.Stderr, "pin: %s: %v\n", bin, err)
			return 1
		}
		n++
	}
	fmt.Printf("relinked %d shims in %s\n", n, binDir())
	return 0
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
