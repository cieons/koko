package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
)

var secretCommands = []Command{
	{
		Noun:    "secret",
		Verb:    "ls",
		Summary: "List the names of the Secrets in a Space, or in every Space you can act in, never a value",
		Args:    []Arg{{Name: "space", Optional: true}},
		Tier:    Read,
		Op:      api.SecretList,
		Run:     action(secretLs),
	},
	{
		Noun:    "secret",
		Verb:    "show",
		Summary: "Print a Secret's value on stdout as stored, to pipe into the command that needs it",
		Args:    []Arg{{Name: "secret", Qualifier: "space"}},
		// It prints a credential (ADR 0019): a Secret's value, which no
		// other command prints.
		Tier: Irreversible,
		Op:   api.SecretShow,
		Run:  action(secretShow),
	},
	{
		Noun:    "secret",
		Verb:    "add",
		Summary: "Store a new Secret, its value read from stdin, a file or a prompt with echo off",
		Args:    []Arg{{Name: "secret", Qualifier: "space"}},
		Flags: []Flag{
			{Name: "from-file", Value: "file", Summary: "read the value from this file, byte for byte"},
		},
		ReadsSecret: true,
		Tier:        Reversible,
		Op:          api.SecretAdd,
		Run:         action(secretAdd),
	},
	{
		Noun:    "secret",
		Verb:    "set",
		Summary: "Replace a Secret's value, read as secret add reads one; the old value is gone",
		Args:    []Arg{{Name: "secret", Qualifier: "space"}},
		Flags: []Flag{
			{Name: "from-file", Value: "file", Summary: "read the value from this file, byte for byte"},
		},
		ReadsSecret: true,
		// Reversible by the verb rule of ADR 0019, though P0 keeps no value
		// history (P0 spec: Secrets).
		Tier: Reversible,
		Op:   api.SecretSet,
		Run:  action(secretSet),
	},
	{
		Noun:    "secret",
		Verb:    "rename",
		Summary: "Give a Secret a new name within its Space",
		Args:    []Arg{{Name: "secret", Qualifier: "space"}, {Name: "new-name"}},
		Tier:    Reversible,
		Op:      api.SecretRename,
		Run:     action(secretRename),
	},
	{
		Noun:    "secret",
		Verb:    "move",
		Summary: "Move a Secret to another Space under the same name; you must be able to act in both",
		Args:    []Arg{{Name: "secret", Qualifier: "space"}, {Name: "space"}},
		Tier:    Reversible,
		Op:      api.SecretMove,
		Run:     action(secretMove),
	},
	{
		Noun:    "secret",
		Verb:    "rm",
		Summary: "Delete a Secret and its value, for good",
		Args:    []Arg{{Name: "secret", Qualifier: "space"}},
		Tier:    Irreversible,
		Op:      api.SecretRemove,
		Run:     action(secretRm),
	},
}

// secretJSON is a Secret in the CLI's own --json: its name in its Space,
// never its value.
type secretJSON struct {
	ID        string `json:"id"`
	Space     string `json:"space"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func secretFromAPI(s api.Secret) secretJSON {
	return secretJSON{ID: s.ID, Space: s.Space, Name: s.Name, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

// qualified is the Secret's name as a command takes it, <space>/<secret>.
func (s secretJSON) qualified() string {
	return s.Space + "/" + s.Name
}

var secretMappings = []mapping{
	{api: reflect.TypeFor[api.SecretListResponse]()},
	{api: reflect.TypeFor[api.SecretShowResponse]()},
	{api: reflect.TypeFor[api.SecretAddResponse]()},
	{api: reflect.TypeFor[api.SecretSetResponse]()},
	{api: reflect.TypeFor[api.SecretRenameResponse]()},
	{api: reflect.TypeFor[api.SecretMoveResponse]()},
	{api: reflect.TypeFor[api.SecretRemoveResponse]()},
	{api: reflect.TypeFor[api.Secret](), cli: reflect.TypeFor[secretJSON]()},
}

// secretValueInput is the secret input of the value of the Secret named
// secret, as <space>/<secret>.
func secretValueInput(secret string) SecretInput {
	return SecretInput{What: "the value of " + secret, Stdin: true, FileFlag: "from-file", Max: api.SecretMaxSize}
}

// secretList is what secret ls prints: the Secrets, as a bare array.
type secretList []secretJSON

func (l secretList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No Secrets.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "NAME\tID")
	for _, s := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", s.qualified(), s.ID)
	}
	return tw.Flush()
}

func secretLs(ctx context.Context, in *Input) (secretList, error) {
	resp, err := in.Call(ctx, api.SecretList, api.SecretListRequest{Space: in.Arg("space")})
	if err != nil {
		return nil, err
	}
	l := make(secretList, 0, len(resp.Secrets))
	for _, s := range resp.Secrets {
		l = append(l, secretFromAPI(s))
	}
	return l, nil
}

// secretValueJSON is a Secret's value in the CLI's own --json: standard
// base64 of its bytes, as encoding/json/v2 writes a byte slice (ADR 0028).
type secretValueJSON []byte

// shownSecret is what secret show prints: for people, the value alone, as
// stored, so that it pipes into another command; with --json, the Secret
// with its value in base64, the one --json that holds a value.
type shownSecret struct {
	secretJSON
	Value secretValueJSON `json:"value_base64"`
}

func (s shownSecret) Human(w io.Writer) error {
	_, err := w.Write(s.Value)
	return err
}

func secretShow(ctx context.Context, in *Input) (shownSecret, error) {
	space, name := in.Qualified("secret")
	resp, err := in.Call(ctx, api.SecretShow, api.SecretShowRequest{Space: space, Name: name})
	if err != nil {
		return shownSecret{}, err
	}
	return shownSecret{secretJSON: secretFromAPI(resp.Secret), Value: secretValueJSON(resp.Value)}, nil
}

// addedSecret is what secret add prints: the Secret it stored, without its
// value.
type addedSecret secretJSON

func (s addedSecret) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Added Secret %s (%s).\n", secretJSON(s).qualified(), s.ID)
	return err
}

func secretAdd(ctx context.Context, in *Input) (addedSecret, error) {
	value, err := in.ReadSecret(ctx, secretValueInput(in.Arg("secret")))
	if err != nil {
		return addedSecret{}, err
	}
	space, name := in.Qualified("secret")
	resp, err := in.Call(ctx, api.SecretAdd, api.SecretAddRequest{Space: space, Name: name, Value: value})
	if err != nil {
		return addedSecret{}, err
	}
	return addedSecret(secretFromAPI(resp.Secret)), nil
}

// setSecret is what secret set prints: the Secret it changed, without its
// value.
type setSecret secretJSON

func (s setSecret) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Set the value of Secret %s (%s).\n", secretJSON(s).qualified(), s.ID)
	return err
}

func secretSet(ctx context.Context, in *Input) (setSecret, error) {
	value, err := in.ReadSecret(ctx, secretValueInput(in.Arg("secret")))
	if err != nil {
		return setSecret{}, err
	}
	space, name := in.Qualified("secret")
	resp, err := in.Call(ctx, api.SecretSet, api.SecretSetRequest{Space: space, Name: name, Value: value})
	if err != nil {
		return setSecret{}, err
	}
	return setSecret(secretFromAPI(resp.Secret)), nil
}

// renamedSecret is what secret rename prints: the Secret under its new
// name, without its value.
type renamedSecret struct {
	secretJSON
	// from is the name it was called by, <space>/<secret>, for people only.
	from string
}

func (s renamedSecret) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Renamed Secret %s to %s (%s).\n", s.from, s.qualified(), s.ID)
	return err
}

func secretRename(ctx context.Context, in *Input) (renamedSecret, error) {
	space, name := in.Qualified("secret")
	resp, err := in.Call(ctx, api.SecretRename, api.SecretRenameRequest{Space: space, Name: name, NewName: in.Arg("new-name")})
	if err != nil {
		return renamedSecret{}, err
	}
	return renamedSecret{secretJSON: secretFromAPI(resp.Secret), from: in.Arg("secret")}, nil
}

// movedSecret is what secret move prints: the Secret in its new Space,
// without its value.
type movedSecret struct {
	secretJSON
	// from is the name it was called by, <space>/<secret>, for people only.
	from string
}

func (s movedSecret) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Moved Secret %s to %s (%s).\n", s.from, s.qualified(), s.ID)
	return err
}

func secretMove(ctx context.Context, in *Input) (movedSecret, error) {
	space, name := in.Qualified("secret")
	resp, err := in.Call(ctx, api.SecretMove, api.SecretMoveRequest{Space: space, Name: name, NewSpace: in.Arg("space")})
	if err != nil {
		return movedSecret{}, err
	}
	return movedSecret{secretJSON: secretFromAPI(resp.Secret), from: in.Arg("secret")}, nil
}

// removedSecret is what secret rm prints: the Secret it deleted, as it was,
// in secret ls's shape and without its value, so a script keeps the id of
// what is gone.
type removedSecret secretJSON

func (s removedSecret) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Removed Secret %s (%s).\n", secretJSON(s).qualified(), s.ID)
	return err
}

func secretRm(ctx context.Context, in *Input) (removedSecret, error) {
	space, name := in.Qualified("secret")
	resp, err := in.Call(ctx, api.SecretRemove, api.SecretRemoveRequest{Space: space, Name: name})
	if err != nil {
		return removedSecret{}, err
	}
	return removedSecret(secretFromAPI(resp.Secret)), nil
}
