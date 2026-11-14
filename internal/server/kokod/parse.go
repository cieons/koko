package kokod

import (
	"slices"
	"strings"
)

// parsed is one command line as parsed: the command it names, with its
// flags' values, or a request for help.
type parsed struct {
	// cmd is the command; nil with help for kokod's own help.
	cmd *command
	// help asks for cmd's help, or kokod's.
	help bool
	json bool
	// values are the flags' values: given, from their variable or their
	// default.
	values map[string]string
}

// value returns the flag name's value, as readSettings reads it.
func (p *parsed) value(name string) string {
	return p.values[name]
}

// parse turns argv into the command it names with its flags' values, or
// refuses it with a usage refusal, whose messages read as koko's parser's.
// getenv supplies the KOKO_* variables of flags not given.
func parse(argv []string, getenv func(string) string) (*parsed, error) {
	if len(argv) == 0 {
		return nil, usagef("give a command: %s; kokod help lists them", commandNames())
	}
	switch argv[0] {
	case "--help":
		return &parsed{help: true}, nil
	case "help":
		// help prints text, so --json changes only how a refusal prints.
		words := slices.DeleteFunc(slices.Clone(argv[1:]), func(a string) bool { return a == "--json" })
		switch len(words) {
		case 0:
			return &parsed{help: true}, nil
		case 1:
			cmd := find(words[0])
			if cmd == nil {
				return nil, usagef("kokod %s is not a command; kokod help takes %s", words[0], commandNames())
			}
			return &parsed{cmd: cmd, help: true}, nil
		}
		return nil, usagef("unexpected argument %q: write kokod help or kokod help <command>", words[1])
	}
	cmd := find(argv[0])
	if cmd == nil {
		if strings.HasPrefix(argv[0], "-") {
			return nil, usagef("give a command before its flags: %s", commandNames())
		}
		return nil, usagef("kokod %s is not a command; kokod takes %s", argv[0], commandNames())
	}
	return cmd.parse(argv[1:], getenv)
}

// parse reads the command's flags from rest, then takes each flag not given
// from its variable, when set, or its default.
func (c *command) parse(rest []string, getenv func(string) string) (*parsed, error) {
	p := &parsed{cmd: c, values: map[string]string{}}
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if !strings.HasPrefix(tok, "-") {
			return nil, usagef("unexpected argument %q: kokod %s takes flags only", tok, c.name)
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tok, "--"), "=")
		if !strings.HasPrefix(tok, "--") || name == "" {
			return nil, usagef("unknown flag %s for kokod %s", tok, c.name)
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
		f := c.lookup(name)
		switch {
		case f == nil && name == "help" && !hasValue:
			return &parsed{cmd: c, help: true}, nil
		case f == nil:
			return nil, usagef("unknown flag --%s for kokod %s", name, c.name)
		case p.values[name] != "":
			return nil, usagef("--%s is given twice", name)
		case f.Bool && hasValue:
			return nil, usagef("--%s takes no value", name)
		case f.Bool:
			value = "true"
		case !hasValue && (i+1 == len(rest) || strings.HasPrefix(rest[i+1], "--")):
			return nil, usagef("--%s needs a value (write --%s=<%s> for one that starts with --)", name, name, f.Value)
		case !hasValue:
			i++
			value = rest[i]
		}
		if value == "" {
			return nil, usagef("--%s needs a value; it cannot be empty", name)
		}
		if err := checkChoice(f, value, "--"+name); err != nil {
			return nil, err
		}
		p.values[name] = value
	}
	for _, f := range c.flags {
		if p.values[f.Name] != "" {
			continue
		}
		if v := getenv(f.Env); f.Env != "" && v != "" {
			if err := checkChoice(&f, v, f.Env); err != nil {
				return nil, err
			}
			p.values[f.Name] = v
			continue
		}
		p.values[f.Name] = f.Default
	}
	return p, nil
}

// lookup returns the command's flag name, or nil.
func (c *command) lookup(name string) *flag {
	i := slices.IndexFunc(c.flags, func(f flag) bool { return f.Name == name })
	if i < 0 {
		return nil
	}
	return &c.flags[i]
}

// checkChoice refuses a value outside f's choices, when it has any; source
// names where the value was given.
func checkChoice(f *flag, value, source string) error {
	if len(f.Choices) > 0 && !slices.Contains(f.Choices, value) {
		return usagef("%s must be one of %s, not %q", source, strings.Join(f.Choices, ", "), value)
	}
	return nil
}

// find returns the command name, or nil.
func find(name string) *command {
	i := slices.IndexFunc(commands, func(c *command) bool { return c.name == name })
	if i < 0 {
		return nil
	}
	return commands[i]
}

// commandNames lists kokod's commands, help last, for a refusal.
func commandNames() string {
	var names []string
	for _, c := range commands {
		names = append(names, "kokod "+c.name)
	}
	return strings.Join(names, ", ") + " or kokod help"
}
