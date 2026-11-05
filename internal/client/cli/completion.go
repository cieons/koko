package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// shellScript is the part of a completion script that is a shell's own:
// what comes before the shared functions, and the driver after them, which
// hands the shell's words to _koko_complete and offers what it prints.
type shellScript struct {
	head   string
	driver string
}

// shellScripts are the shells koko completion writes a script for: the
// login shells of the Linux and macOS the CLI runs on.
var shellScripts = map[string]shellScript{
	"bash": {head: bashHead, driver: bashDriver},
	"zsh":  {head: zshHead, driver: zshDriver},
}

// completionShells are the names of the shells, the choices of koko
// completion's <shell>.
var completionShells = slices.Sorted(maps.Keys(shellScripts))

// completionJSON is what koko completion prints: the script for a shell.
// For people, the script alone, to load in the shell.
type completionJSON struct {
	Shell  string `json:"shell"`
	Script string `json:"script"`
}

func (c completionJSON) Human(w io.Writer) error {
	_, err := io.WriteString(w, c.Script)
	return err
}

func completion(_ context.Context, in *Input) (completionJSON, error) {
	shell := in.Arg("shell")
	script := shellScripts[shell]
	var b strings.Builder
	b.WriteString(script.head)
	writeCompletionTables(&b, in.tree)
	b.WriteString(completeScript)
	b.WriteString(script.driver)
	return completionJSON{Shell: shell, Script: b.String()}, nil
}

// writeCompletionTables writes the shell functions that hold the tree's
// part of completion, each a case on a command path:
//
//   - _koko_next: the words that can follow the path, such as the verbs of
//     a noun;
//   - _koko_flags: the flags of the command at the path, --json among them;
//   - _koko_value: for a flag that takes a value, its choices, or :file,
//     :dir or :value (any value) when it has none; nothing for one that
//     takes no value;
//   - _koko_arg: the choices of the command's positional argument at the
//     index given, from 1.
//
// The functions are written in what bash and zsh share.
func writeCompletionTables(b *strings.Builder, tree []Command) {
	next := map[string][]string{}
	flags := map[string][]string{}
	values := map[string]string{}
	args := map[string]string{}
	for i := range tree {
		c := &tree[i]
		w := c.words()
		for j := range w {
			prefix := strings.Join(w[:j], " ")
			if !slices.Contains(next[prefix], w[j]) {
				next[prefix] = append(next[prefix], w[j])
			}
		}
		path := c.Path()
		flags[path] = []string{"--json"}
		for _, f := range c.flags() {
			flags[path] = append(flags[path], "--"+f.Name)
			if !f.Bool {
				values[path+" --"+f.Name] = flagValues(f)
			}
		}
		for j, a := range c.Args {
			if len(a.Choices) > 0 {
				args[fmt.Sprintf("%s %d", path, j+1)] = strings.Join(a.Choices, " ")
			}
		}
	}
	writeCase(b, "_koko_next", "the words that can follow a command path", `"$1"`, joinWords(next))
	writeCase(b, "_koko_flags", "the flags of a command", `"$1"`, joinWords(flags))
	writeCase(b, "_koko_value", "the values of a command's flag: choices, or :file, :dir or :value", `"$1 $2"`, values)
	writeCase(b, "_koko_arg", "the choices of a command's argument, by its index from 1", `"$1 $2"`, args)
}

// flagValues is what completes a flag's value: its choices, or what its
// value names.
func flagValues(f Flag) string {
	switch {
	case len(f.Choices) > 0:
		return strings.Join(f.Choices, " ")
	case f.Value == "file", f.Value == "dir":
		return ":" + f.Value
	}
	return ":value"
}

func joinWords(m map[string][]string) map[string]string {
	joined := map[string]string{}
	for k, words := range m {
		joined[k] = strings.Join(slices.Sorted(slices.Values(words)), " ")
	}
	return joined
}

// writeCase writes a shell function that prints the value of the case
// whose key subject matches, by key.
func writeCase(b *strings.Builder, name, about, subject string, cases map[string]string) {
	fmt.Fprintf(b, "\n# %s prints %s.\n%s() {\n\tcase %s in\n", name, about, name, subject)
	for _, k := range slices.Sorted(maps.Keys(cases)) {
		fmt.Fprintf(b, "\t%s) echo %s ;;\n", shellJoin([]string{k}), shellJoin([]string{cases[k]}))
	}
	b.WriteString("\tesac\n}\n")
}

const bashHead = `# koko completion for bash, from koko completion bash.
# Load it from ~/.bashrc with: eval "$(koko completion bash)"
`

const zshHead = `#compdef koko
# koko completion for zsh, from koko completion zsh.
# Load it from ~/.zshrc, after compinit, with: eval "$(koko completion zsh)"
# or save it as _koko in a directory of $fpath.
`

// completeScript holds the shell functions both shells share, which walk
// the words of a command line and print what can follow.
const completeScript = `
# _koko_is_next tells whether the word $2 can follow the command path $1.
_koko_is_next() {
	case " $(_koko_next "$1") " in
	*" $2 "*) return 0 ;;
	esac
	return 1
}

# _koko_complete prints what can take the place of the last of the words
# given, the words after koko: words, or :file, :dir or :value for a flag's
# value.
_koko_complete() {
	local cmd= n=0 value= w
	while [ $# -gt 1 ]; do
		w=$1
		shift
		if [ -n "$value" ]; then
			value=
		elif [ "${w#--}" != "$w" ]; then
			case $w in
			*=*) ;;
			*) value=$(_koko_value "$cmd" "$w") ;;
			esac
		elif [ "$n" -eq 0 ] && _koko_is_next "$cmd" "$w"; then
			cmd=${cmd:+$cmd }$w
		else
			n=$((n + 1))
		fi
	done
	if [ -n "$value" ]; then
		echo "$value"
		return
	fi
	case $1 in
	-*) _koko_flags "$cmd" ;;
	*)
		if [ "$n" -eq 0 ]; then
			_koko_next "$cmd"
		fi
		_koko_arg "$cmd" $((n + 1))
		;;
	esac
}
`

const bashDriver = `
_koko() {
	local cur=${COMP_WORDS[COMP_CWORD]} out
	out=$(_koko_complete "${COMP_WORDS[@]:1:COMP_CWORD}")
	case $out in
	:file | :dir)
		local IFS=$'\n'
		COMPREPLY=($(compgen -"${out:1:1}" -- "$cur"))
		type compopt >/dev/null 2>&1 && compopt -o filenames
		;;
	:value) COMPREPLY=() ;;
	*) COMPREPLY=($(compgen -W "$out" -- "$cur")) ;;
	esac
}

complete -F _koko koko
`

const zshDriver = `
_koko() {
	local out
	out=$(_koko_complete "${(@)words[2,CURRENT]}")
	case $out in
	:file) _files ;;
	:dir) _files -/ ;;
	:value) ;;
	*) compadd -- ${=out} ;;
	esac
}

if [ "$funcstack[1]" = "_koko" ]; then
	_koko "$@"
else
	compdef _koko koko
fi
`
