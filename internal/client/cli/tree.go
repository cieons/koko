package cli

import (
	"context"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
)

// Command is one entry of the command tree: everything the parser, help,
// completion and `koko skill` need to know about a command, and what it runs.
type Command struct {
	// Noun is the command's noun: "space", or "space member" for a nested
	// one, or "" for a top-level verb such as version.
	Noun string
	// Verb is what the command does to the noun, such as "add" or "ls".
	Verb string
	// Summary says in one line what the command does.
	Summary string
	// Args are the positional arguments, in order; each is required unless
	// it is Optional.
	Args []Arg
	// Flags are the command's flags. Every command also takes --json, which is
	// not listed.
	Flags []Flag
	// Pairs makes the command take key=value words beside its arguments, such
	// as region=eu: the fields and Attributes it writes. An empty value is
	// refused, so region=$REGION with REGION unset never clears one.
	Pairs bool
	// Unset makes the command take --unset <key>, repeatable, to clear a
	// field or Attribute. It is not listed in Flags.
	Unset bool
	// Filters makes a list take filter terms beside its arguments, each a
	// word holding '=' or '.' (price.gt=10, region!=eu, expires_at.le=+30d,
	// price.has), and --sort <key>[.desc], repeatable, which is not listed in
	// Flags. Reading the terms is the command's (readFilters).
	Filters bool
	// ReadsSecret says the command reads a secret input (Input.ReadSecret),
	// such as a Secret value, which is never an argument: a word its command
	// line does not expect is refused without being repeated, since it may
	// be the secret written where an argument goes.
	ReadsSecret bool
	// Tier is the command's risk tier (ADR 0019). It is required.
	Tier Tier
	// OwnerOnly says only the Owner may run the command, for help and koko
	// skill. The koko server's access check enforces it, refusing anyone
	// else with owner_only.
	OwnerOnly bool
	// Op is the API operation the command calls, or nil for a command that
	// calls none.
	Op api.Operation
	// Run carries the command out; it also holds the command's --json shape.
	Run Action
}

// Path is the command as typed after koko, such as "space add".
func (c *Command) Path() string {
	return strings.TrimSpace(c.Noun + " " + c.Verb)
}

func (c *Command) words() []string {
	return strings.Fields(c.Path())
}

// Arg is a positional argument.
type Arg struct {
	// Name is shown as <Name>, such as <space>.
	Name string
	// Qualifier, when set, makes the argument a qualified name written
	// <Qualifier>/<Name>, such as <space>/<secret>, which the parser splits
	// on its one '/'. No name holds '/'.
	Qualifier string
	// Bare lets a qualified argument also be written as <Name> alone, as a
	// User names their own device.
	Bare bool
	// Optional lets the argument be left out, shown as [<Name>]. Only
	// arguments after every required one may be optional.
	Optional bool
	// Many makes the last argument take every word left, one or more, shown
	// as <Name>..., such as the Users of space member add; Input.Args
	// returns them in order.
	Many bool
}

// Pair is a key=value word, cut at its first '='.
type Pair struct {
	Key   string
	Value string
}

// Flag is a flag, always written after the command path.
type Flag struct {
	// Name is the flag without its dashes, such as "data-dir".
	Name string
	// Bool makes the flag take no value.
	Bool bool
	// Value names the value in help, such as "dir"; empty for a Bool flag.
	Value string
	// Env is the flag's KOKO_* environment variable, used when the flag is not
	// given, or empty.
	Env string
	// Default is the value when neither the flag nor Env is given.
	Default string
	// Choices, when set, are the only values accepted.
	Choices []string
	// Summary says in one line what the flag does.
	Summary string
}

// Tier is a command's risk tier: its path alone says what it can do, so an
// agent's harness can allow or ask by command prefix (ADR 0019).
type Tier int

const (
	tierUnset Tier = iota
	// Read only reads.
	Read
	// Reversible changes the record in a way that can be undone.
	Reversible
	// Irreversible cannot be undone, reaches a Host or prints a credential.
	Irreversible
)

func (t Tier) String() string {
	switch t {
	case Read:
		return "read"
	case Reversible:
		return "reversible"
	case Irreversible:
		return "irreversible"
	}
	return "unset"
}

// Result is what a command prints on success: it is marshalled as the
// command's --json, the CLI's own contract (ADR 0016), and Human writes it for
// people, which is no contract.
type Result interface {
	Human(w io.Writer) error
}

// Action is what a command runs, with the type of what it prints.
type Action struct {
	run   func(ctx context.Context, in *Input) (Result, error)
	shape reflect.Type
}

// Shape is the type of the command's --json, or nil for a command that prints
// nothing.
func (a Action) Shape() reflect.Type {
	return a.shape
}

// action makes the Action of a command that prints an R.
func action[R Result](run func(ctx context.Context, in *Input) (R, error)) Action {
	return Action{
		run: func(ctx context.Context, in *Input) (Result, error) {
			return run(ctx, in)
		},
		shape: reflect.TypeFor[R](),
	}
}

// longAction makes the Action of a command that runs until it is stopped,
// such as koko web: run prints its R once, as soon as it is ready, through
// ready, and nothing more when it ends. A failure after that still goes to
// stderr with its exit status.
func longAction[R Result](run func(ctx context.Context, in *Input, ready func(R) error) error) Action {
	return Action{
		run: func(ctx context.Context, in *Input) (Result, error) {
			return nil, run(ctx, in, func(r R) error {
				return writeResult(in.inv.Stdout, r, in.line.json)
			})
		},
		shape: reflect.TypeFor[R](),
	}
}

// quietAction makes the Action of a command that prints nothing on success.
func quietAction(run func(ctx context.Context, in *Input) error) Action {
	return Action{run: func(ctx context.Context, in *Input) (Result, error) {
		return nil, run(ctx, in)
	}}
}

// commands lists every area's commands; an area's first ticket adds its line.
var commands = [][]Command{
	serverCommands,
	spaceCommands,
	versionCommands,
	deviceCommands,
	userCommands,
	webCommands,
	joinCommands,
	hostCommands,
	secretCommands,
	statusCommands,
	attrCommands,
	enrollCommands,
	serviceCommands,
}

// Tree returns every command.
func Tree() []Command {
	return slices.Concat(commands...)
}
