package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/record"
)

// The commands generated from the command tree: koko skill, the reference
// for agents of this build, its stub, koko help and koko completion (P0
// spec: overview). They read only the binary, so they need no koko server.
var skillCommands = []Command{
	{
		Verb: "skill",
		Summary: "Print the reference for agents of this koko: its commands by risk tier with their --json shapes, " +
			"the filter syntax, the Preset Attributes, the error codes and the exit statuses",
		Tier: Read,
		Run:  action(skill),
	},
	{
		Noun:    "skill",
		Verb:    "install",
		Summary: "Write the stub SKILL.md that tells an agent to run koko skill, as <dir>/koko/SKILL.md",
		Flags: []Flag{{
			Name: "dir", Value: "dir",
			Summary: "The agent's skills directory, in which koko writes koko/SKILL.md; ~/.claude/skills when not given",
		}},
		Tier: Reversible,
		Run:  action(skillInstall),
	},
	{
		Verb:    "help",
		Summary: "List the commands; show the command the words name in full, with its flags, or list every command they start",
		Args:    []Arg{{Name: "command", Optional: true, Many: true}},
		Tier:    Read,
		Run:     action(help),
	},
	{
		Verb:    "completion",
		Summary: "Print the script that completes koko's commands, flags and their values in a shell",
		Args:    []Arg{{Name: "shell", Choices: completionShells}},
		Tier:    Read,
		Run:     action(completion),
	},
}

// skillJSON is what koko skill prints: the reference for agents of this
// build, generated from the command tree and the registries.
type skillJSON struct {
	// Version is this koko's version, as koko version prints it.
	Version string `json:"version"`
	// Commands are every command, by risk tier and then by path.
	Commands []commandJSON `json:"commands"`
	// Filters is the syntax of the filter terms a list takes.
	Filters filtersJSON `json:"filters"`
	// Presets are the Preset Attributes, in the order of their table.
	Presets []keyJSON `json:"presets"`
	// ErrorCodes are every error code: the koko server's, then the CLI's own.
	ErrorCodes []errorCodeJSON `json:"error_codes"`
	// ExitStatuses are the exit statuses, each with what it means.
	ExitStatuses []exitStatusJSON `json:"exit_statuses"`
}

// commandJSON is one command of the tree, as koko skill and koko help
// print it.
type commandJSON struct {
	// Command is the command path, such as "space add".
	Command string `json:"command"`
	// Usage is how the command is written, such as "koko space add <space>".
	Usage   string `json:"usage"`
	Summary string `json:"summary"`
	// Tier is read, reversible or irreversible (ADR 0019).
	Tier      string `json:"tier"`
	OwnerOnly bool   `json:"owner_only"`
	// Args are the positional arguments, in order.
	Args []argJSON `json:"args"`
	// Flags are the flags beside --json, which every command takes.
	Flags []flagJSON `json:"flags"`
	// Filters are the fields a list filters and sorts on, beside every
	// Attribute key, or null for a command that takes no filter terms.
	Filters *[]keyJSON `json:"filters"`
	// JSON is the shape of what the command prints with --json, or null for
	// a command that prints nothing.
	JSON *string `json:"json"`

	// tier is Tier's, to order and group by.
	tier Tier
	// remote says the command runs a remote command (RemoteCommand): it
	// takes no flag, not even --json, and every word after its other
	// arguments is that command's.
	remote bool
	// stdio says the command carries a byte stream on its stdio (Stdio):
	// it takes no flag, not even --json.
	stdio bool
	// longer are the paths of the longer commands this one's path starts,
	// which koko help names beside it.
	longer []string
}

// argJSON is a positional argument.
type argJSON struct {
	// Arg is the argument as usage writes it, such as "<space>/<secret>".
	Arg string `json:"arg"`
	// Choices are the only values it takes, or null for any.
	Choices *[]string `json:"choices"`
}

// flagJSON is a flag.
type flagJSON struct {
	// Flag is the flag as usage writes it, such as "--note-file <file>".
	Flag    string `json:"flag"`
	Summary string `json:"summary"`
	// Env is the KOKO_* variable the flag takes when it is not given, or
	// null.
	Env *string `json:"env"`
	// Default is the value when neither the flag nor its variable is given,
	// or null.
	Default *string `json:"default"`
	// Choices are the only values it takes, or null for any.
	Choices *[]string `json:"choices"`
	// Repeatable says the flag may be given more than once.
	Repeatable bool `json:"repeatable"`
}

// keyJSON is a key of the record and the type of its values: a field a
// list filters on, or a Preset Attribute.
type keyJSON struct {
	Key string `json:"key"`
	// Type is text, number, date or enum.
	Type string `json:"type"`
	// Values are an enum's values, in the order they sort in, or null.
	Values *[]string `json:"values"`
	// Min is the least value of a number that has one, such as price's 0,
	// or null.
	Min *record.Value `json:"min"`

	// typeText writes the type for people, such as "number, at least 0" or
	// "enum: no, yes".
	typeText string
}

// filtersJSON is the syntax of filter terms.
type filtersJSON struct {
	// Terms are the forms of a term, each with what it matches.
	Terms []termJSON `json:"terms"`
	// RelativeDates are the forms a date's value may also take, in a filter
	// or a write, resolved in the caller's time zone.
	RelativeDates string `json:"relative_dates"`
}

// termJSON is one form of a filter term.
type termJSON struct {
	Term    string `json:"term"`
	Matches string `json:"matches"`
}

// errorCodeJSON is an error code.
type errorCodeJSON struct {
	Code string `json:"code"`
	// From is "server" for the koko server's codes, which the CLI passes
	// through, or "cli" for the CLI's own, a problem on this device.
	From string `json:"from"`
}

// exitStatusJSON is an exit status and what it means.
type exitStatusJSON struct {
	Status int    `json:"status"`
	Means  string `json:"means"`
}

func skill(_ context.Context, in *Input) (skillJSON, error) {
	tree := slices.Clone(in.tree)
	slices.SortStableFunc(tree, func(a, b Command) int {
		return cmp.Or(cmp.Compare(a.Tier, b.Tier), cmp.Compare(a.Path(), b.Path()))
	})
	s := skillJSON{Version: build.Read().Version}
	for i := range tree {
		s.Commands = append(s.Commands, commandOf(&tree[i]))
	}
	for _, t := range filterTerms {
		s.Filters.Terms = append(s.Filters.Terms, termJSON{Term: t.form, Matches: t.matches})
	}
	s.Filters.RelativeDates = relativeDateForms
	for _, p := range record.Presets() {
		s.Presets = append(s.Presets, keyOf(p.Key, p.Type))
	}
	for _, c := range api.Codes() {
		s.ErrorCodes = append(s.ErrorCodes, errorCodeJSON{Code: c.Name, From: "server"})
	}
	for _, c := range cliCodes {
		s.ErrorCodes = append(s.ErrorCodes, errorCodeJSON{Code: c, From: "cli"})
	}
	s.ExitStatuses = slices.Clone(exitStatuses)
	return s, nil
}

// commandOf describes a command of the tree.
func commandOf(c *Command) commandJSON {
	j := commandJSON{
		Command:   c.Path(),
		Usage:     synopsis(c),
		Summary:   c.Summary,
		Tier:      c.Tier.String(),
		tier:      c.Tier,
		remote:    c.RemoteCommand,
		stdio:     c.Stdio,
		OwnerOnly: c.OwnerOnly,
		Args:      []argJSON{},
		Flags:     []flagJSON{},
	}
	for _, a := range c.Args {
		j.Args = append(j.Args, argJSON{Arg: a.usage(), Choices: orNull(a.Choices)})
	}
	for _, f := range c.flags() {
		j.Flags = append(j.Flags, flagJSON{
			Flag:       f.usage(),
			Summary:    f.Summary,
			Env:        orNull(f.Env),
			Default:    orNull(f.Default),
			Choices:    orNull(f.Choices),
			Repeatable: f.repeatable(),
		})
	}
	if c.Filters != nil {
		keys := []keyJSON{}
		for _, k := range slices.Sorted(maps.Keys(c.Filters)) {
			keys = append(keys, keyOf(k, c.Filters[k]))
		}
		j.Filters = &keys
	}
	if t := c.Run.Shape(); t != nil {
		j.JSON = new(shape(t))
	}
	return j
}

// orNull is v, or nil for an empty v, which --json prints as null.
func orNull[T ~string | ~[]string](v T) *T {
	if len(v) == 0 {
		return nil
	}
	return &v
}

// keyOf describes a key whose values are of type t.
func keyOf(key string, t record.Type) keyJSON {
	k := keyJSON{Key: key, Type: string(t.Kind()), typeText: string(t.Kind())}
	if values := t.Values(); values != nil {
		k.Values = &values
		k.typeText += ": " + t.DescribeValues()
	}
	if m, ok := t.Min(); ok {
		k.Min = &m
		k.typeText += ", at least " + m.String()
	}
	return k
}

// flags returns every flag the command takes beside --json: --unset and
// --sort when it takes them, then its own.
func (c *Command) flags() []Flag {
	var flags []Flag
	if c.Unset {
		flags = append(flags, unsetFlag)
	}
	if c.Filters != nil {
		flags = append(flags, sortFlag)
	}
	return append(flags, c.Flags...)
}

// repeatable tells whether the flag may be given more than once.
func (f *Flag) repeatable() bool {
	return f.Name == unsetFlag.Name || f.Name == sortFlag.Name
}

// usage shows how the flag is written: --with-note, or --dir <dir>.
func (f *Flag) usage() string {
	if f.Bool {
		return "--" + f.Name
	}
	return "--" + f.Name + " <" + f.Value + ">"
}

// synopsis shows how a command is written in full, such as "koko host ls
// [<filter>...] [--sort <key[.desc]>]... [--with-note]".
func synopsis(c *Command) string {
	words := []string{"koko", c.Path()}
	for _, a := range c.Args {
		words = append(words, a.usage())
	}
	if c.Pairs {
		words = append(words, "[<key>=<value>...]")
	}
	if c.Filters != nil {
		words = append(words, "[<filter>...]")
	}
	for _, f := range c.flags() {
		u := "[" + f.usage() + "]"
		if f.repeatable() {
			u += "..."
		}
		words = append(words, u)
	}
	return strings.Join(words, " ")
}

// The meaning of each risk tier, for koko skill.
var tierMeanings = map[Tier]string{
	Read:         "Only reads: allow these by prefix.",
	Reversible:   "Changes the record in a way that can be undone.",
	Irreversible: "Cannot be undone, reaches a Host or prints a credential.",
}

func (s skillJSON) Human(w io.Writer) error {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	p("# koko\n\n")
	p("koko keeps the record of a fleet of rented servers (Hosts with their Attributes, in Spaces, and Secrets)\n")
	p("and is the one door people and agents use to reach them. This is the reference for koko %s,\n", s.Version)
	p("generated from the binary you run, so it matches it exactly.\n\n")

	p("## The command line\n\n")
	p("- Every command is `koko <noun> <verb>`, or a verb alone such as `koko status`. Flags come after\n")
	p("  the command path: `koko space ls --json`, never `koko --json space ls`.\n")
	p("- The command path carries the risk, so a harness can allow or ask by prefix. No flag raises a\n")
	p("  command's risk.\n")
	p("- A qualified name is written with `/`, which no name holds: `<space>/<secret>`, `<user>/<device>`.\n")
	p("- Fields and Attributes are written `key=value`; an empty value is refused, and `--unset <key>`\n")
	p("  clears one.\n")
	p("- koko never asks \"are you sure?\". It prompts only on a terminal, and only for a secret input it\n")
	p("  was not given; without a terminal it fails at once with exit status %d, naming the flag or stdin\n", exitRefused)
	p("  form. A Secret value is never an argument.\n")
	p("- A flag not given takes its `KOKO_*` variable, where it lists one, then its default.\n")
	for _, c := range s.Commands {
		if c.remote {
			p("- `%s` runs one command on a Host as ssh does: every word after\n", c.Usage)
			p("  `%s` is the command's, as typed, flags included, joined with spaces for the Host Account's\n", c.Args[len(c.Args)-2].Arg)
			p("  shell. It takes no flag, not even `--json`, passes stdin, stdout and stderr through, and exits\n")
			p("  with the command's status, or %d when there is none or koko itself failed.\n", exitRemote)
		}
		if c.stdio {
			p("- `%s` is the ProxyCommand of koko's ssh_config, for ssh, not for direct use: it\n", c.Usage)
			p("  carries ssh's connection to the Host on its stdin and stdout, and takes no flag, not even\n")
			p("  `--json`. Run `ssh <host>.koko`, or `koko exec`, instead.\n")
		}
	}
	p("- Every command %stakes `--json`. On success stdout holds the result; on\n", butNoJSON(s.Commands, "`"))
	p("  failure stdout is empty and stderr holds the error, with `--json` as\n")
	p("  `{\"error\": {\"code\": ..., \"message\": ...}}`. A refused\n")
	p("  batch, such as several Hosts on stdin, adds `\"items\"`, each `{\"index\", \"code\", \"message\"}`:\n")
	p("  an item at fault, by its place in the batch.\n")
	p("- A list prints a bare JSON array of every item, a write the record it made or changed, and every\n")
	p("  record carries an `id`. Within a major version fields are only added: ignore those you do not know.\n")
	p("- A command the koko server lacks fails with `%s`, naming the koko server's version and\n", codeServerTooOld)
	p("  the command; nothing was sent.\n\n")

	p("## Commands by risk tier\n\n")
	p("Each command shows how it is written, what it does, its flags and the shape of its `--json`, written\n")
	p("as: %s. (Owner) marks a command only the Owner may run.\n", shapeNotation)
	for _, c := range s.Commands {
		for _, d := range s.Commands {
			if strings.HasPrefix(d.Command, c.Command+" ") && d.tier > c.tier {
				p("\nA rule that allows the prefix `koko %s` (%s) also allows `koko %s` (%s).\n", c.Command, c.Tier, d.Command, d.Tier)
			}
		}
	}
	tier := tierUnset
	shapes := map[string]string{}
	for _, c := range s.Commands {
		if c.tier != tier {
			tier = c.tier
			p("\n### %s\n\n%s\n\n", tier, tierMeanings[tier])
		}
		owner := ""
		if c.OwnerOnly {
			owner = " (Owner)"
		}
		p("- `%s`%s: %s\n", c.Usage, owner, c.Summary)
		for _, a := range c.Args {
			if a.Choices != nil {
				p("  - `%s`: one of %s\n", a.Arg, strings.Join(*a.Choices, ", "))
			}
		}
		for _, f := range c.Flags {
			p("  - `%s`: %s%s\n", f.Flag, f.Summary, f.details())
		}
		if c.Filters != nil {
			var fields []string
			for _, k := range *c.Filters {
				fields = append(fields, "`"+k.Key+"` ("+k.typeText+")")
			}
			p("  - Filters and sorts on %s, and any Attribute key\n", strings.Join(fields, ", "))
		}
		if c.JSON != nil {
			p("  - `--json`: %s\n", c.shapeOnce(shapes))
		}
	}

	p("\n## Filtering and sorting lists\n\n")
	p("A list that takes filter terms reads every word holding `=` or `.` as one, and lists the records\n")
	p("that match every term. No term needs quoting in a shell.\n\n")
	for _, t := range s.Filters.Terms {
		p("- `%s`: %s\n", t.Term, t.Matches)
	}
	p("\nA term compares by the key's type (below). A record that lacks the key matches only `key!=value`\n")
	p("and `key.lacks`, and sorts last. `--sort <key>` sorts by a key and `--sort <key>.desc` from the\n")
	p("greatest value down; repeat it to sort ties. A date's value may also be relative, in a filter or\n")
	p("a write: %s, counted from today in the time zone `TZ` names.\n", s.Filters.RelativeDates)
	p("For example, `koko host ls expires_at.le=+30d auto_renew=no --sort expires_at` lists the Hosts\n")
	p("due for renewal, soonest first.\n\n")

	p("## Value types\n\n")
	p("- text: 1 byte to %d KiB of UTF-8 on one line, compared in natural order (`web2` before `web10`)\n", record.MaxText>>10)
	p("- number: an exact decimal of at most %d digits, %d of them after the point, compared by value;\n",
		record.MaxDecimalDigits, record.MaxDecimalFraction)
	p("  a JSON number in `--json`, every other value a JSON string\n")
	p("- date: `YYYY-MM-DD`, compared by the calendar\n")
	p("- enum: one of its values, compared in the order they are listed\n\n")

	p("## Preset Attributes\n\n")
	p("These Attribute keys have fixed types; every other key holds text. `koko attr ls` also lists the\n")
	p("keys in use.\n\n")
	for _, k := range s.Presets {
		p("- `%s`: %s\n", k.Key, k.typeText)
	}

	p("\n## Error codes\n\n")
	p("Branch on an error's code, never its message. The koko server's codes, which the CLI passes through:\n")
	p("%s.\n\n", codeList(s.ErrorCodes, "server"))
	p("The CLI's own, for a problem on this device or a command line it refused: %s.\n", codeList(s.ErrorCodes, "cli"))

	p("\n## Exit statuses\n\n")
	p("They never change meaning.\n\n")
	for _, e := range s.ExitStatuses {
		p("- `%d`: %s\n", e.Status, e.Means)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// shapeOnce writes the command's --json shape the first time a command of
// the text prints it, and afterwards names that command, so that the many
// commands printing a Host spell it out once. seen holds, by shape, how a
// later command names it, and gains this command's shape, and the shape of
// one item when it prints an array.
func (c commandJSON) shapeOnce(seen map[string]string) string {
	s := *c.JSON
	if named, ok := seen[s]; ok {
		return named
	}
	seen[s] = "as `koko " + c.Command + "` prints"
	if item, ok := strings.CutPrefix(s, "["); ok && strings.HasSuffix(item, "]") {
		item = strings.TrimSuffix(item, "]")
		if _, ok := seen[item]; !ok {
			seen[item] = "one item of what `koko " + c.Command + "` prints"
		}
	}
	return "`" + s + "`"
}

// details says what else to know of a flag: its choices, its default and
// its variable.
func (f flagJSON) details() string {
	var d []string
	if f.Choices != nil {
		d = append(d, "one of "+strings.Join(*f.Choices, ", "))
	}
	if f.Default != nil {
		d = append(d, "default "+*f.Default)
	}
	if f.Env != nil {
		d = append(d, "or `"+*f.Env+"`")
	}
	if len(d) == 0 {
		return ""
	}
	return " (" + strings.Join(d, "; ") + ")"
}

// codeList writes the codes from one source as a list.
func codeList(codes []errorCodeJSON, from string) string {
	var names []string
	for _, c := range codes {
		if c.From == from {
			names = append(names, "`"+c.Code+"`")
		}
	}
	return strings.Join(names, ", ")
}

// skillStub is the SKILL.md koko skill install writes. It only says to run
// koko skill, so it never goes stale across upgrades, and its bytes do not
// depend on the build.
const skillStub = `---
name: koko
description: Drive koko, the CLI that keeps the record of a fleet of rented servers (Hosts with their Attributes such as provider, price and expiry, in Spaces, and Secrets) and reaches them over SSH. Use it whenever a task names koko or involves those servers, their renewals or their Secrets.
---

Run ` + "`koko skill`" + ` for the reference of the installed koko before any other koko command: its
commands by risk tier, their ` + "`--json`" + ` shapes, the filter syntax, the Preset Attributes, the
error codes and the exit statuses. It is generated from the binary, so it always matches the koko
you run.
`

// skillInstalledJSON is what koko skill install prints: where it wrote the
// stub.
type skillInstalledJSON struct {
	// Path is the stub's absolute path, <dir>/koko/SKILL.md.
	Path string `json:"path"`
}

func (s skillInstalledJSON) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Wrote the koko skill's stub to %s; an agent that reads it runs koko skill.\n", s.Path)
	return err
}

// skillInstall writes the stub into the koko directory of the skills
// directory --dir names, or of ~/.claude/skills: a directory koko owns, so
// it never overwrites another skill's SKILL.md.
func skillInstall(_ context.Context, in *Input) (skillInstalledJSON, error) {
	dir := in.Flag("dir")
	if dir == "" {
		home := in.inv.getenv("HOME")
		if home == "" {
			return skillInstalledJSON{}, usagef("HOME is not set, so koko cannot find ~/.claude/skills: give --dir <dir>")
		}
		dir = filepath.Join(home, ".claude", "skills")
	}
	dir, err := filepath.Abs(filepath.Join(dir, "koko"))
	if err != nil {
		return skillInstalledJSON{}, fmt.Errorf("find the koko skill's directory: %w", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := writeFileAtomically(path, []byte(skillStub)); err != nil {
		return skillInstalledJSON{}, fmt.Errorf("write the koko skill's stub %s: %w", path, err)
	}
	return skillInstalledJSON{Path: path}, nil
}

// writeFileAtomically writes data as the file at path, readable by all,
// making its directory if need be, through a temporary file renamed over
// it, so that a reader sees the old file or the new one, never a part.
func writeFileAtomically(path string, data []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(0o644)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
