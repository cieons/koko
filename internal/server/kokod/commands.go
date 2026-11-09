package kokod

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/serverrun"
)

// command is one of kokod's commands.
type command struct {
	name    string
	summary string
	// flags are the command's flags besides --json, which every command
	// takes; json says what --json prints.
	flags []serverrun.Flag
	json  string
	run   func(ctx context.Context, p *Program, inv *Invocation, line *parsed) error
}

// commands are kokod's commands besides help, in the order help lists them.
// The installer's machine steps are koko server install's and koko server
// uninstall's.
var commands = []*command{
	{
		name:    "run",
		summary: "Run the koko server in the foreground, as its systemd unit and the image do",
		flags:   serverrun.Flags,
		json:    "Print a refusal as JSON",
		run:     run,
	},
	{
		name:    "version",
		summary: "Print this kokod's build",
		json:    "Print the build as JSON",
		run:     version,
	},
}

// run is kokod run: it runs the koko server in the foreground until ctx
// ends, logging on stderr.
func run(ctx context.Context, p *Program, inv *Invocation, line *parsed) error {
	s, err := serverrun.Read(line.value, inv.getenv)
	if err != nil {
		return err
	}
	return p.Serve(ctx, server.Config{
		DataDir:       s.DataDir,
		RootKeyPath:   s.RootKeyPath,
		RootKeySource: s.RootKeySource,
		Owner:         s.Owner,
		Address:       s.Address,
		Listen:        s.Listen,
		LogLevel:      s.LogLevel,
		Log:           inv.Stderr,
	})
}

// versionJSON is what kokod version --json prints, as koko version --json
// prints its own: this binary's build.
type versionJSON struct {
	// Version is the module version, such as v0.3.0, or "(devel)".
	Version string `json:"version"`
	// Revision is the commit it was built from, or empty when unknown.
	Revision string `json:"revision"`
	// Go is the Go release that built it.
	Go string `json:"go"`
	// Platform is GOOS/GOARCH, such as linux/amd64.
	Platform string `json:"platform"`
}

// version is kokod version: it prints the build koko version prints, naming
// kokod.
func version(_ context.Context, _ *Program, inv *Invocation, line *parsed) error {
	b := build.Read()
	var out strings.Builder
	if line.json {
		if err := encodeJSON(&out, versionJSON(b)); err != nil {
			return fmt.Errorf("encode the build: %w", err)
		}
	} else {
		fmt.Fprintf(&out, "kokod %s\n", b)
	}
	_, err := io.WriteString(inv.Stdout, out.String())
	return err
}

// writeHelp writes kokod's help: its commands, and what runs the
// installer's machine steps.
func writeHelp(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "kokod is the koko server, on Linux; koko is the CLI that people and agents run.")
	_, _ = fmt.Fprintln(tw, "\nCommands:")
	for _, c := range commands {
		_, _ = fmt.Fprintf(tw, "  kokod %s\t%s\n", c.name, c.summary)
	}
	_, _ = fmt.Fprintf(tw, "  kokod help [<command>]\tPrint this help, or a command's, as <command> --help does\n")
	_, _ = fmt.Fprintln(tw, "\nTo install, upgrade or uninstall the koko server on this machine, run\n"+
		"koko server install or koko server uninstall as root; they do the machine's part.")
	_ = tw.Flush()
}

// writeHelp writes the command's help: its usage line, what it does, and
// its flags with their variables, defaults and choices.
func (c *command) writeHelp(w io.Writer) {
	line := "kokod " + c.name
	for _, f := range c.flags {
		line += fmt.Sprintf(" [--%s <%s>]", f.Name, f.Value)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "%s [--json]\n\n%s\n\nFlags:\n", line, c.summary)
	for _, f := range c.flags {
		_, _ = fmt.Fprintf(tw, "  --%s <%s>\t%s%s\n", f.Name, f.Value, f.Summary, details(f))
	}
	_, _ = fmt.Fprintf(tw, "  --json\t%s\n", c.json)
	_ = tw.Flush()
}

// details is what help adds after a flag's summary: its choices, its
// default and its variable, as koko's help writes them.
func details(f serverrun.Flag) string {
	var d []string
	if f.Choices != nil {
		d = append(d, "one of "+strings.Join(f.Choices, ", "))
	}
	if f.Default != "" {
		d = append(d, "default "+f.Default)
	}
	if f.Env != "" {
		d = append(d, "or `"+f.Env+"`")
	}
	if len(d) == 0 {
		return ""
	}
	return " (" + strings.Join(d, "; ") + ")"
}
