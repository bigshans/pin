package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Completion is split in two halves: `pin __complete` prints candidates for a
// given command line, and `pin completion <shell>` prints the shell glue that
// calls it. Keeping the logic in Go means one implementation for every shell.

var verbs = []string{
	"add", "a", "ls", "l", "rm", "r", "which", "w", "doctor", "d",
	"shims", "s", "completion", "version", "v", "help", "h",
}

var flags = []string{"-n", "--node", "--runtime", "--install", "--fix"}

// shells are the argument values accepted by `pin completion`.
var shells = []string{"bash", "zsh", "fish", "powershell"}

// runtimeNames returns every installed runtime version, across all managers.
func runtimeNames() []string {
	seen := map[string]bool{}
	var out []string
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
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	sort.Strings(out)
	return out
}

// pinnedNames returns tool keys followed by every pinned bin name.
func pinnedNames() []string {
	db, err := loadDB()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(db)*2)
	keys := make([]string, 0, len(db))
	for k := range db {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
		for _, b := range db[k].Bins {
			if !seen[b] {
				seen[b] = true
				out = append(out, b)
			}
		}
	}
	return out
}

func filterPrefix(in []string, prefix string) []string {
	if prefix == "" {
		return in
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// candidates maps the words typed so far (after the binary name) to suggestions.
func candidates(words []string) []string {
	last, prev := "", ""
	if n := len(words); n > 0 {
		last = words[n-1]
		if n > 1 {
			prev = words[n-2]
		}
	}

	switch prev {
	case "-n", "-r", "--node", "--runtime":
		return filterPrefix(runtimeNames(), last)
	}
	if i := strings.IndexAny(last, "="); i > 0 {
		switch last[:i] {
		case "-n", "--node", "--runtime":
			return filterPrefix(runtimeNames(), last[i+1:])
		}
	}

	if strings.HasPrefix(last, "-") {
		return filterPrefix(flags, last)
	}
	if len(words) <= 1 {
		return filterPrefix(verbs, last)
	}

	switch words[0] {
	case "r", "rm", "remove", "u", "uninstall", "w", "which":
		return filterPrefix(pinnedNames(), last)
	case "d", "doctor":
		return filterPrefix([]string{"--fix"}, last)
	case "completion":
		return filterPrefix(shells, last)
	}
	return nil
}

// cmdComplete is the hidden backend (`pin __complete ...`).
//
// The trailing "--cursor" marker means "the word under the cursor is a fresh,
// empty word". Shells that cannot pass an empty argument (PowerShell, fish)
// use it instead of an empty string.
func cmdComplete(args []string) int {
	if n := len(args); n > 0 && args[n-1] == "--cursor" {
		args = args[:n-1]
		if len(args) == 0 || args[len(args)-1] != "" {
			args = append(args, "")
		}
	}
	for _, c := range candidates(args) {
		fmt.Println(c)
	}
	return 0
}

func cmdCompletion(args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: pin completion <%s>\n", strings.Join(shells, "|"))
		return 2
	}
	var body string
	switch args[0] {
	case "bash":
		body = bashCompletion
	case "zsh":
		body = zshCompletion
	case "fish":
		body = fishCompletion
	case "powershell", "pwsh":
		body = pwshCompletion
	default:
		fmt.Fprintf(os.Stderr, "pin: unknown shell %q\n", args[0])
		return 2
	}
	fmt.Print(body)
	return 0
}

const bashCompletion = `# pin bash completion. Add to ~/.bashrc:
#   eval "$(pin completion bash)"
_pin_complete() {
    local IFS=$'\n'
    COMPREPLY=($(pin __complete "${COMP_WORDS[@]:1:COMP_CWORD}"))
}
complete -o default -F _pin_complete pin
`

const zshCompletion = `# pin zsh completion. Add to ~/.zshrc:
#   eval "$(pin completion zsh)"
_pin() {
    local -a cands args
    args=(${words[2,-1]})
    [[ -z "${words[CURRENT]}" ]] && args+=--cursor
    cands=(${(f)"$(pin __complete ${args[@]})"})
    compadd -a cands
}
compdef _pin pin
`

const fishCompletion = `# pin fish completion. Add to ~/.config/fish/config.fish:
#   pin completion fish | source
function __pin_complete
    set -l tokens (commandline -opc)[2..-1]
    set -l current (commandline -ct)
    if test -n "$current"
        pin __complete $tokens $current
    else
        pin __complete $tokens --cursor
    end
end
complete -c pin -f -a '(__pin_complete)'
`

const pwshCompletion = `# pin PowerShell completion. Add to your $PROFILE:
#   pin completion powershell | Out-String | Invoke-Expression
Register-ArgumentCompleter -Native -CommandName pin -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $words = @($commandAst.CommandElements | Select-Object -Skip 1 | ForEach-Object { $_.ToString() })
    if ($wordToComplete -and ($words.Count -eq 0 -or $words[-1] -ne $wordToComplete)) {
        $words += $wordToComplete
    } elseif (-not $wordToComplete) {
        $words += '--cursor'
    }
    pin __complete @words | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`
