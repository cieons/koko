package cli

import (
	"regexp"
	"slices"
	"strings"
)

// parsed is a command line the parser accepted.
type parsed struct {
	cmd   *Command
	args  map[string]string
	flags map[string]string
	json  bool
}

// parse turns argv into the command it names, with its arguments and flags,
// or refuses it with a usageError. It never calls the koko server. getenv
// supplies the KOKO_* equivalents of flags that were not given.
func parse(tree []Command, argv []string, getenv func(string) string) (*parsed, error) {
	if len(argv) == 0 {
		return nil, usagef("give a command: koko <noun> <verb>, such as koko space ls")
	}
	cmd, path, early, rest := locate(tree, argv)
	if cmd == nil {
		return nil, unknownCommand(tree, path, rest)
	}
	if len(early) > 0 {
		fixed := slices.Concat([]string{"koko"}, path, rest, early)
		return nil, usagef("flags come after the command path: write %s", shellJoin(fixed))
	}

	p := &parsed{cmd: cmd, args: map[string]string{}, flags: map[string]string{}}
	var positional []string
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if !isFlag(tok) {
			positional = append(positional, tok)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tok, "--"), "=")
		if !strings.HasPrefix(tok, "--") || name == "" {
			return nil, usagef("unknown flag %s for koko %s", tok, cmd.Path())
		}
		if name == "json" {
			if hasValue {
				return nil, usagef("--json takes no value")
			}
			if p.json {
				return nil, usagef("--json is given twice")
			}
			p.json = true
			continue
		}
		f := cmd.flag(name)
		switch {
		case f == nil:
			return nil, usagef("unknown flag --%s for koko %s", name, cmd.Path())
		case p.flags[name] != "":
			return nil, usagef("--%s is given twice", name)
		case f.Bool && hasValue:
			return nil, usagef("--%s takes no value", name)
		case f.Bool:
			p.flags[name] = "true"
			continue
		case !hasValue && (i+1 == len(rest) || strings.HasPrefix(rest[i+1], "--")):
			return nil, usagef("--%s needs a value (write --%s=<%s> for one that starts with --)", name, name, f.Value)
		case !hasValue:
			i++
			value = rest[i]
		}
		if value == "" {
			return nil, usagef("--%s needs a value; it cannot be empty", name)
		}
		if err := f.check(value, "--"+name); err != nil {
			return nil, err
		}
		p.flags[name] = value
	}

	if len(positional) < len(cmd.Args) {
		return nil, usagef("koko %s needs <%s>", cmd.Path(), cmd.Args[len(positional)].Name)
	}
	if len(positional) > len(cmd.Args) {
		return nil, usagef("unexpected argument %q: %s", positional[len(cmd.Args)], usageLine(cmd))
	}
	for i, a := range cmd.Args {
		if positional[i] == "" {
			return nil, usagef("<%s> cannot be empty in koko %s", a.Name, cmd.Path())
		}
		p.args[a.Name] = positional[i]
	}

	for _, f := range cmd.Flags {
		if p.flags[f.Name] != "" || f.Bool {
			continue
		}
		if v := getenv(f.Env); f.Env != "" && v != "" {
			if err := f.check(v, f.Env); err != nil {
				return nil, err
			}
			p.flags[f.Name] = v
			continue
		}
		p.flags[f.Name] = f.Default
	}
	return p, nil
}

// locate finds the command argv names. It reads the command path word by
// word, as long as the words so far start some command's path, and collects
// the flags written before the path is complete, which includes a flag after
// a command whose path a later word extends (koko skill --json install). It
// returns the command (or nil), the path words it read, those early flags,
// and the rest of argv.
func locate(tree []Command, argv []string) (cmd *Command, path, early, rest []string) {
	i := 0
	for ; i < len(argv); i++ {
		tok := argv[i]
		if isFlag(tok) {
			if find(tree, path) != nil && !extendedLater(tree, path, argv[i:]) {
				break
			}
			early = append(early, tok)
			if takesValue(tree, tok) && i+1 < len(argv) {
				i++
				early = append(early, argv[i])
			}
			continue
		}
		next := append(slices.Clone(path), tok)
		if !startsAPath(tree, next) {
			break
		}
		path = next
	}
	return find(tree, path), path, early, argv[i:]
}

// unknownCommand says why argv names no command, and what the koko it began
// with could continue with.
func unknownCommand(tree []Command, path, rest []string) error {
	var next []string
	for i := range tree {
		w := tree[i].words()
		if len(w) > len(path) && slices.Equal(w[:len(path)], path) && !slices.Contains(next, w[len(path)]) {
			next = append(next, w[len(path)])
		}
	}
	slices.Sort(next)
	typed := strings.Join(path, " ")
	switch {
	case len(rest) > 0 && !isFlag(rest[0]) && len(path) == 0:
		return usagef("koko %s is not a command; commands start with %s", rest[0], strings.Join(next, ", "))
	case len(rest) > 0 && !isFlag(rest[0]):
		return usagef("koko %s %s is not a command; koko %s takes %s", typed, rest[0], typed, strings.Join(next, ", "))
	case len(path) == 0:
		return usagef("give a command before its flags: koko <noun> <verb>, such as koko space ls")
	}
	return usagef("koko %s needs one of %s", typed, strings.Join(next, ", "))
}

// extendedLater tells whether the first word in rest that is neither a flag
// nor a flag's value extends path to a longer command.
func extendedLater(tree []Command, path, rest []string) bool {
	for j := 0; j < len(rest); j++ {
		if !isFlag(rest[j]) {
			return startsAPath(tree, append(slices.Clone(path), rest[j]))
		}
		if takesValue(tree, rest[j]) {
			j++
		}
	}
	return false
}

func isFlag(tok string) bool {
	return strings.HasPrefix(tok, "-") && tok != "-"
}

// takesValue tells whether a flag written as tok, with no "=value", takes the
// next word as its value in some command of the tree.
func takesValue(tree []Command, tok string) bool {
	name, ok := strings.CutPrefix(tok, "--")
	if !ok || strings.Contains(name, "=") {
		return false
	}
	for i := range tree {
		if f := tree[i].flag(name); f != nil && !f.Bool {
			return true
		}
	}
	return false
}

func startsAPath(tree []Command, words []string) bool {
	return slices.ContainsFunc(tree, func(c Command) bool {
		w := c.words()
		return len(w) >= len(words) && slices.Equal(w[:len(words)], words)
	})
}

func find(tree []Command, path []string) *Command {
	i := slices.IndexFunc(tree, func(c Command) bool { return slices.Equal(c.words(), path) })
	if i < 0 || len(path) == 0 {
		return nil
	}
	return &tree[i]
}

func (c *Command) flag(name string) *Flag {
	i := slices.IndexFunc(c.Flags, func(f Flag) bool { return f.Name == name })
	if i < 0 {
		return nil
	}
	return &c.Flags[i]
}

// check refuses a value outside the flag's choices; source names where the
// value came from: the flag or its environment variable.
func (f *Flag) check(value, source string) error {
	if len(f.Choices) > 0 && !slices.Contains(f.Choices, value) {
		return usagef("%s must be one of %s, not %q", source, strings.Join(f.Choices, ", "), value)
	}
	return nil
}

// usageLine shows how a command is written, such as "koko space add <space>".
func usageLine(c *Command) string {
	if len(c.Args) == 0 {
		return "koko " + c.Path() + " takes no arguments"
	}
	line := "koko " + c.Path()
	for _, a := range c.Args {
		line += " <" + a.Name + ">"
	}
	return "write " + line
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

// shellJoin writes words as one shell command line, quoting where needed.
func shellJoin(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		if shellSafe.MatchString(w) {
			quoted[i] = w
		} else {
			quoted[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}
