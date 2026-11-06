package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
)

// helpList is what koko help prints: the commands it was asked about, by
// path, each as koko skill describes it. For people, one command is shown
// in full and several as a table.
type helpList []commandJSON

// help returns the command the words name, with the paths of the longer
// commands it starts, such as koko skill's koko skill install; or else
// every command whose path starts with the words; every command for none.
func help(_ context.Context, in *Input) (helpList, error) {
	words := in.Args("command")
	tree := slices.Clone(in.tree)
	slices.SortFunc(tree, func(a, b Command) int { return cmp.Compare(a.Path(), b.Path()) })
	var starting []*Command
	for i := range tree {
		if w := tree[i].words(); len(w) >= len(words) && slices.Equal(w[:len(words)], words) {
			starting = append(starting, &tree[i])
		}
	}
	if c := find(tree, words); c != nil {
		j := commandOf(c)
		for _, d := range starting {
			if d != c {
				j.longer = append(j.longer, d.Path())
			}
		}
		return helpList{j}, nil
	}
	l := helpList{}
	for _, c := range starting {
		l = append(l, commandOf(c))
	}
	if len(l) == 0 {
		_, path, _, rest := locate(tree, words)
		return nil, unknownCommand(tree, path, rest)
	}
	return l, nil
}

func (l helpList) Human(w io.Writer) error {
	if len(l) == 1 {
		return l[0].help(w)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "Usage: koko <noun> <verb> [<argument>...] [<flag>...]")
	_, _ = fmt.Fprintln(tw)
	for _, c := range l {
		args := []string{c.Command}
		for _, a := range c.Args {
			args = append(args, a.Arg)
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", strings.Join(args, " "), c.Summary)
	}
	_, _ = fmt.Fprintln(tw)
	_, _ = fmt.Fprintf(tw, "Every command %stakes --json. Run koko help <command> for its flags,\n", butNoJSON(l, ""))
	_, _ = fmt.Fprintln(tw, "and koko skill for the reference for agents.")
	return tw.Flush()
}

// butNoJSON names the commands of cs that take no --json, each between
// quote, as "but koko exec and koko tunnel ", or "" for none.
func butNoJSON(cs []commandJSON, quote string) string {
	var noJSON []string
	for _, c := range cs {
		if c.remote || c.stdio {
			noJSON = append(noJSON, quote+"koko "+c.Command+quote)
		}
	}
	if len(noJSON) == 0 {
		return ""
	}
	return "but " + strings.Join(noJSON, " and ") + " "
}

// help writes one command in full, for people.
func (c commandJSON) help(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintf(tw, "%s\n\n%s\n\n", c.Usage, c.Summary)
	owner := ""
	if c.OwnerOnly {
		owner = " Only the Owner may run it."
	}
	_, _ = fmt.Fprintf(tw, "Risk tier: %s.%s\n", c.Tier, owner)
	for _, a := range c.Args {
		if a.Choices != nil {
			_, _ = fmt.Fprintf(tw, "\n%s is one of %s.\n", a.Arg, strings.Join(*a.Choices, ", "))
		}
	}
	switch {
	case c.remote:
		_, _ = fmt.Fprintf(tw, "\nIt takes no flag, not even --json: every word after %s is the command's, as typed,\n"+
			"joined with spaces for the Host Account's shell, as ssh joins them.\n", c.Args[len(c.Args)-2].Arg)
	case c.stdio:
		_, _ = fmt.Fprintln(tw, "\nIt takes no flag, not even --json: ssh runs it as the ProxyCommand of koko's ssh_config,\n"+
			"and it carries ssh's connection on its stdin and stdout.")
	default:
		_, _ = fmt.Fprintln(tw, "\nFlags:")
		for _, f := range c.Flags {
			_, _ = fmt.Fprintf(tw, "  %s\t%s%s\n", f.Flag, f.Summary, f.details())
		}
		_, _ = fmt.Fprintf(tw, "  --json\tPrint the result as JSON\n")
	}
	if c.Filters != nil {
		var fields []string
		for _, k := range *c.Filters {
			fields = append(fields, k.Key+" ("+k.typeText+")")
		}
		_, _ = fmt.Fprintf(tw, "\nFilters and sorts on %s, and any Attribute key; koko skill gives the syntax.\n",
			strings.Join(fields, ", "))
	}
	for _, path := range c.longer {
		_, _ = fmt.Fprintf(tw, "\nkoko %s is a longer command: run koko help %s.\n", path, path)
	}
	return tw.Flush()
}
