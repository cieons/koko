package cli

import (
	"regexp"
	"slices"
	"strings"
)

// parsed is a command line the parser accepted.
type parsed struct {
	cmd  *Command
	args map[string]string
	// many holds the words of the argument that takes many, in order.
	many      []string
	qualified map[string]qualifiedName
	flags     map[string]string
	pairs     []Pair
	unset     []string
	// terms are the filter terms of a command with Filters, and sorts the
	// values of its --sort, each in order.
	terms []string
	sorts []string
	json  bool
}

// qualifiedName is a qualified argument's two parts: ops and db-password for
// ops/db-password. The qualifier is empty for one written bare.
type qualifiedName struct {
	qualifier string
	name      string
}

// parse turns argv into the command it names, with its arguments and flags,
// or refuses it with a usageError. It never calls the koko server. getenv
// supplies the KOKO_* equivalents of flags that were not given.
func parse(tree []Command, argv []string, getenv func(string) string) (*parsed, error) {
	if len(argv) == 0 {
		return nil, usagef("give a command: koko <noun> <verb>, such as koko space ls; koko help lists them")
	}
	cmd, path, early, rest := locate(tree, argv)
	if cmd != nil && cmd.RemoteCommand {
		return parseRemote(cmd, early, rest)
	}
	if slices.Contains(early, "--help") {
		return nil, noHelpFlag(path)
	}
	if cmd == nil {
		return nil, unknownCommand(tree, path, rest)
	}
	if len(early) > 0 {
		fixed := slices.Concat([]string{"koko"}, path, rest, early)
		return nil, usagef("flags come after the command path: write %s", shellJoin(fixed))
	}

	p := &parsed{cmd: cmd, args: map[string]string{}, qualified: map[string]qualifiedName{}, flags: map[string]string{}}
	var positional []string
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if !isFlag(tok) {
			if cmd.Filters != nil && strings.ContainsAny(tok, "=.") {
				p.terms = append(p.terms, tok)
				continue
			}
			if key, value, ok := strings.Cut(tok, "="); ok && cmd.Pairs {
				if err := p.addPair(key, value); err != nil {
					return nil, err
				}
				continue
			}
			positional = append(positional, tok)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tok, "--"), "=")
		if !strings.HasPrefix(tok, "--") || name == "" {
			if cmd.ReadsSecret {
				return nil, usagef("unknown flag for koko %s, not repeated here in case it is a secret", cmd.Path())
			}
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
		case f == nil && name == "help" && !hasValue:
			return nil, noHelpFlag(path)
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
		if f == &unsetFlag {
			if slices.Contains(p.unset, value) {
				return nil, usagef("--unset %s is given twice", value)
			}
			p.unset = append(p.unset, value)
			continue
		}
		if f == &sortFlag {
			p.sorts = append(p.sorts, value)
			continue
		}
		p.flags[name] = value
	}
	for _, key := range p.unset {
		if p.hasPair(key) {
			return nil, usagef("%s= and --unset %s are both given", key, key)
		}
	}

	if len(positional) < len(cmd.Args) && !cmd.Args[len(positional)].Optional {
		return nil, usagef("koko %s needs %s", cmd.Path(), cmd.Args[len(positional)].usage())
	}
	if len(positional) > len(cmd.Args) && cmd.many() == nil {
		if cmd.ReadsSecret {
			return nil, usagef("unexpected argument, not repeated here in case it is a secret, which koko never takes as an argument: %s",
				usageLine(cmd))
		}
		return nil, usagef("unexpected argument %q: %s", positional[len(cmd.Args)], usageLine(cmd))
	}
	for i, v := range positional {
		a := cmd.Args[min(i, len(cmd.Args)-1)]
		if v == "" {
			return nil, usagef("%s cannot be empty in koko %s", a.usage(), cmd.Path())
		}
		if err := checkChoice(a.Choices, v, a.usage()); err != nil {
			return nil, err
		}
		if a.Many {
			p.many = append(p.many, v)
			continue
		}
		if a.Qualifier != "" {
			q, err := a.split(v)
			if err != nil {
				return nil, err
			}
			p.qualified[a.Name] = q
		}
		p.args[a.Name] = v
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

// parseRemote reads the command line of a command with RemoteCommand: its
// arguments before the last, none of them a flag, then every word left, as
// typed, as its last, where --help reaches the remote command. It refuses a
// flag before the last argument, early among them, since such a command
// takes none (--help pointing to koko help), and a missing or empty
// argument; each refusal exits 255.
func parseRemote(cmd *Command, early, rest []string) (*parsed, error) {
	refuse := func(err error) (*parsed, error) {
		return nil, &remoteFailure{err: err}
	}
	fixed := cmd.Args[:len(cmd.Args)-1]
	before := slices.Concat(early, rest[:min(len(fixed), len(rest))])
	if slices.Contains(before, "--help") {
		return refuse(noHelpFlag(cmd.words()))
	}
	if slices.ContainsFunc(before, isFlag) {
		return refuse(usagef("koko %s takes no flags, not even --json: %s", cmd.Path(), usageLine(cmd)))
	}
	if len(rest) <= len(fixed) {
		return refuse(usagef("koko %s needs %s: %s", cmd.Path(), cmd.Args[len(rest)].usage(), usageLine(cmd)))
	}
	p := &parsed{cmd: cmd, args: map[string]string{}, qualified: map[string]qualifiedName{}, flags: map[string]string{}}
	for i, a := range fixed {
		if rest[i] == "" {
			return refuse(usagef("%s cannot be empty in koko %s", a.usage(), cmd.Path()))
		}
		p.args[a.Name] = rest[i]
	}
	p.many = rest[len(fixed):]
	return p, nil
}

// addPair takes one key=value word, refusing an empty key or value and a key
// given twice.
func (p *parsed) addPair(key, value string) error {
	switch {
	case key == "":
		return usagef("=%s has no key: write <key>=%s", value, value)
	case value == "" && p.cmd.Unset:
		return usagef("the value of %s= cannot be empty; to clear %s, write --unset %s", key, key, key)
	case value == "":
		return usagef("the value of %s= cannot be empty", key)
	case p.hasPair(key):
		return usagef("%s= is given twice", key)
	}
	p.pairs = append(p.pairs, Pair{Key: key, Value: value})
	return nil
}

func (p *parsed) hasPair(key string) bool {
	return slices.ContainsFunc(p.pairs, func(pr Pair) bool { return pr.Key == key })
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

// noHelpFlag refuses --help, where the parser would refuse it as an unknown
// flag, pointing to koko help for the command path typed.
func noHelpFlag(path []string) error {
	return usagef("there is no --help flag: run %s", shellJoin(append([]string{"koko", "help"}, path...)))
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

// many returns the command's argument that takes many words, its last, or
// nil when it has none.
func (c *Command) many() *Arg {
	if len(c.Args) == 0 || !c.Args[len(c.Args)-1].Many {
		return nil
	}
	return &c.Args[len(c.Args)-1]
}

// unsetFlag is --unset <key>, which a command with Unset takes, repeatable.
var unsetFlag = Flag{Name: "unset", Value: "key", Summary: "Clear a field or Attribute; repeatable"}

// sortFlag is --sort <key>[.desc], which a command with Filters takes,
// repeatable: each sorts the ties of the one before.
var sortFlag = Flag{Name: "sort", Value: "key[.desc]", Summary: "Sort by a field or Attribute, from the greatest value down with .desc; repeatable"}

func (c *Command) flag(name string) *Flag {
	if c.Unset && name == unsetFlag.Name {
		return &unsetFlag
	}
	if c.Filters != nil && name == sortFlag.Name {
		return &sortFlag
	}
	i := slices.IndexFunc(c.Flags, func(f Flag) bool { return f.Name == name })
	if i < 0 {
		return nil
	}
	return &c.Flags[i]
}

// check refuses a value outside the flag's choices; source names where the
// value came from: the flag or its environment variable.
func (f *Flag) check(value, source string) error {
	return checkChoice(f.Choices, value, source)
}

// checkChoice refuses a value outside choices, when there are any; source
// names where the value was given.
func checkChoice(choices []string, value, source string) error {
	if len(choices) > 0 && !slices.Contains(choices, value) {
		return usagef("%s must be one of %s, not %q", source, strings.Join(choices, ", "), value)
	}
	return nil
}

// split cuts a qualified argument at its one '/', refusing an empty part, a
// part that holds '/', and a bare name where the qualifier is required.
func (a *Arg) split(v string) (qualifiedName, error) {
	q, n, ok := strings.Cut(v, "/")
	switch {
	case !ok && a.Bare:
		return qualifiedName{name: v}, nil
	case !ok:
		return qualifiedName{}, usagef("%q is not %s: the <%s> is missing", v, a.usage(), a.Qualifier)
	case q == "":
		return qualifiedName{}, usagef("%q is not %s: the <%s> is empty", v, a.usage(), a.Qualifier)
	case n == "":
		return qualifiedName{}, usagef("%q is not %s: the <%s> is empty", v, a.usage(), a.Name)
	case strings.Contains(n, "/"):
		return qualifiedName{}, usagef("%q is not %s: a name cannot hold /", v, a.usage())
	}
	return qualifiedName{qualifier: q, name: n}, nil
}

// usage shows how the argument is written: <space>, <space>/<secret>,
// [<user>/]<device> for one that may be bare, <user>... for one that takes
// many words, and in brackets for an optional one, such as [<device>].
func (a *Arg) usage() string {
	var u string
	switch {
	case a.Qualifier != "" && a.Bare:
		u = "[<" + a.Qualifier + ">/]<" + a.Name + ">"
	case a.Qualifier != "":
		u = "<" + a.Qualifier + ">/<" + a.Name + ">"
	default:
		u = "<" + a.Name + ">"
	}
	if a.Many {
		u += "..."
	}
	if a.Optional {
		return "[" + u + "]"
	}
	return u
}

// usageLine shows how a command is written, such as "koko space add <space>".
func usageLine(c *Command) string {
	if len(c.Args) == 0 {
		return "koko " + c.Path() + " takes no arguments"
	}
	line := "koko " + c.Path()
	for _, a := range c.Args {
		line += " " + a.usage()
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
