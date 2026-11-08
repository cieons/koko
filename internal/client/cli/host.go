package cli

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// noteFileFlag is --note-file <file>, or --note-file - for stdin: the Note,
// which never goes through an argument.
var noteFileFlag = Flag{Name: "note-file", Value: "file", Summary: "Set the Note from a file, or from stdin with -"}

var hostCommands = []Command{
	{
		Noun:    "host",
		Verb:    "add",
		Summary: "Record a Host: address= and account= are required, and space= defaults to your Personal Space; without <host>, record the Hosts JSON on stdin holds, in the shape host ls --json prints",
		Args:    []Arg{{Name: "host", Optional: true}},
		Flags:   []Flag{noteFileFlag},
		Pairs:   true,
		Tier:    Reversible,
		Op:      api.HostAdd,
		Run:     action(hostAdd),
	},
	{
		Noun:    "host",
		Verb:    "ls",
		Summary: "List the Hosts you can see that match every filter term, such as expires_at.le=+30d auto_renew=no",
		Flags: []Flag{
			{Name: "with-note", Bool: true, Summary: "Include each Host's Note"},
			{Name: "all", Bool: true, Summary: "Include the Retired Hosts, which state=retired lists alone"},
		},
		Filters: api.HostListFields,
		Tier:    Read,
		Op:      api.HostList,
		Run:     action(hostLs),
	},
	{
		Noun:    "host",
		Verb:    "show",
		Summary: "Show a Host with its Attributes and Note",
		Args:    []Arg{{Name: "host"}},
		Tier:    Read,
		Op:      api.HostShow,
		Run:     action(hostShow),
	},
	{
		Noun:    "host",
		Verb:    "set",
		Summary: "Change the fields, Attributes and Note of a Host that you name, and only those",
		Args:    []Arg{{Name: "host"}},
		Flags:   []Flag{noteFileFlag},
		Pairs:   true,
		Unset:   true,
		Tier:    Reversible,
		Op:      api.HostSet,
		Run:     action(hostSet),
	},
	{
		Noun:    "host",
		Verb:    "rename",
		Summary: "Give a Host a new name, which is its SSH Name",
		Args:    []Arg{{Name: "host"}, {Name: "new-name"}},
		Tier:    Reversible,
		Op:      api.HostRename,
		Run:     action(hostRename),
	},
	{
		Noun:    "host",
		Verb:    "move",
		Summary: "Move a Host to another Space; you must be able to act in both",
		Args:    []Arg{{Name: "host"}, {Name: "space"}},
		Tier:    Reversible,
		Op:      api.HostMove,
		Run:     action(hostMove),
	},
	{
		Noun:    "host",
		Verb:    "retire",
		Summary: "Retire a Host that is gone: it leaves host ls and frees its name, and keeps its record, which its id names from then on",
		Args:    []Arg{{Name: "host"}},
		Tier:    Irreversible,
		Op:      api.HostRetire,
		Run:     action(hostRetire),
	},
	{
		Noun:    "host",
		Verb:    "rm",
		Summary: "Delete a Host that was never Managed, with its Attributes, Note and Services",
		Args:    []Arg{{Name: "host"}},
		Tier:    Irreversible,
		Op:      api.HostRemove,
		Run:     action(hostRm),
	},
}

// hostJSON is a Host in the CLI's own --json: the shape host ls --json
// prints, which host add will read back.
type hostJSON struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Space   string `json:"space"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Account string `json:"account"`
	// State is registered, managed or retired.
	State string `json:"state"`
	// HostKey is the pinned host key's SHA-256 fingerprint, or null for a
	// Host with no pin.
	HostKey *string `json:"host_key"`
	// Attributes sit in their own object, so a field koko adds later never
	// collides with an Attribute key. Each value is typed: a number is a
	// JSON number written exactly as stored, and every other value a JSON
	// string.
	Attributes map[string]record.Value `json:"attributes"`
	// Note is the Host's Note, or null for none. Every command that prints
	// one Host prints it; host ls only with --with-note, and without it
	// leaves the member out: nil here, where a Host with no Note is a
	// pointer to nil.
	Note      **string `json:"note,omitzero"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// hostFromAPI maps a Host from the API, with its Note.
func hostFromAPI(h api.Host) hostJSON {
	j := hostJSON{
		ID:         h.ID,
		Name:       h.Name,
		Space:      h.Space,
		Address:    h.Address,
		Port:       h.Port,
		Account:    h.Account,
		State:      h.State,
		HostKey:    h.HostKey,
		Attributes: maps.Clone(h.Attributes),
		CreatedAt:  h.CreatedAt,
		UpdatedAt:  h.UpdatedAt,
	}
	if j.Attributes == nil {
		j.Attributes = map[string]record.Value{}
	}
	var note *string
	if h.Note != "" {
		note = new(h.Note)
	}
	j.Note = &note
	return j
}

// note returns the Host's Note, and whether it has one.
func (h hostJSON) note() (string, bool) {
	if h.Note == nil || *h.Note == nil {
		return "", false
	}
	return **h.Note, true
}

var hostMappings = []mapping{
	{api: reflect.TypeFor[api.HostAddResponse]()},
	{api: reflect.TypeFor[api.HostListResponse]()},
	{api: reflect.TypeFor[api.HostShowResponse](), omitted: map[string]string{
		"services": "host show prints a Host's Services for people only: its --json is the Host as host ls prints one, " +
			"and service ls host=<host> --json lists them",
	}},
	{api: reflect.TypeFor[api.HostSetResponse]()},
	{api: reflect.TypeFor[api.HostRenameResponse]()},
	{api: reflect.TypeFor[api.HostMoveResponse]()},
	{api: reflect.TypeFor[api.HostRetireResponse]()},
	{api: reflect.TypeFor[api.HostRemoveResponse]()},
	{api: reflect.TypeFor[api.Host](), cli: reflect.TypeFor[hostJSON]()},
}

// hostPairs are the key=value words of host add or host set, sorted into
// the Host's fields and its Attributes.
type hostPairs struct {
	address, account, space *string
	port                    *int
	attributes              map[string]record.Value
}

// readHostPairs sorts the key=value words of koko host <verb> into fields
// and Attributes, refusing before any call a key the command does not take
// and a value that breaks the record's rules, a Preset's value that does
// not parse as its type among them. A relative date, such as
// expires_at=+1y, is resolved in the caller's time zone. Only add takes
// space=.
func readHostPairs(in *Input, verb string) (hostPairs, error) {
	p := hostPairs{attributes: map[string]record.Value{}}
	for _, pr := range in.Pairs() {
		switch {
		case pr.Key == "address":
			address, err := record.Address(pr.Value)
			if err != nil {
				return hostPairs{}, usagef("address=: %v", err)
			}
			p.address = new(address)
		case pr.Key == "port":
			port, err := record.ParsePort(pr.Value)
			if err != nil {
				return hostPairs{}, usagef("port=: %v", err)
			}
			p.port = new(port)
		case pr.Key == "account":
			if err := record.CheckAccount(pr.Value); err != nil {
				return hostPairs{}, usagef("account=: %v", err)
			}
			p.account = new(pr.Value)
		case pr.Key == "space" && verb == "add":
			p.space = new(pr.Value)
		case record.Reserved(pr.Key):
			return hostPairs{}, hostFieldRefusal(pr.Key+"=", pr.Key, verb)
		default:
			if err := record.CheckKey(pr.Key); err != nil {
				return hostPairs{}, usagef("%s=: %v", pr.Key, err)
			}
			v, err := in.parseValue(record.TypeOf(pr.Key), pr.Value)
			if err != nil {
				return hostPairs{}, usagef("the value of %s=: %v", pr.Key, err)
			}
			p.attributes[pr.Key] = v
		}
	}
	return p, nil
}

// hostFieldRefusal refuses written, which gives or clears the field key
// that koko host <verb> does not take, saying how to change it.
func hostFieldRefusal(written, key, verb string) error {
	switch key {
	case "name":
		if verb == "add" {
			return usagef("%s: the Host's name is the argument of koko host add <host>", written)
		}
		return usagef("%s: koko host %s does not rename a Host; write koko host rename <host> <new-name>", written, verb)
	case "space":
		return usagef("%s: koko host %s does not move a Host; write koko host move <host> <space>", written, verb)
	case "state":
		return usagef("%s: koko sets a Host's state itself; to retire a Host that is gone, write koko host retire <host>", written)
	case "note":
		return usagef("%s: the Note is read from a file with --note-file <file>, or from stdin with --note-file -", written)
	}
	if slices.Contains(record.Required, key) {
		return usagef("%s: a Host always has its %s; set another with %s=", written, key, key)
	}
	return usagef("%s: %s is a field koko host %s does not take", written, key, verb)
}

// readNote returns the Note --note-file gives, read from its file or, for
// -, from stdin, or nil when it is not given. It refuses before any call a
// file it cannot read, stdin that is a terminal, which would wait for
// typing nobody may do, and a Note that breaks its rule.
func readNote(in *Input) (*string, error) {
	path := in.Flag(noteFileFlag.Name)
	var (
		r  io.Reader
		at = "--note-file " + path
	)
	switch path {
	case "":
		return nil, nil
	case "-":
		if in.terminal() != nil {
			return nil, usagef("--note-file - reads stdin, which is a terminal: pipe the Note in, or give --note-file <file>")
		}
		r = cmp.Or[io.Reader](in.inv.Stdin, strings.NewReader(""))
	default:
		f, err := os.Open(path)
		if err != nil {
			return nil, usagef("cannot read %s: %v", at, unwrapPath(err))
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	// One byte past the limit is enough to refuse a Note that is too long.
	b, err := io.ReadAll(io.LimitReader(r, record.MaxNote+1))
	if err != nil {
		return nil, usagef("cannot read %s: %v", at, unwrapPath(err))
	}
	note := string(b)
	if err := record.CheckNote(note); err != nil {
		return nil, usagef("%s: %v", at, err)
	}
	return &note, nil
}

// unwrapPath drops the path an *fs.PathError repeats, which the message
// that wraps it names already.
func unwrapPath(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}

// addedHosts is what host add prints: the Hosts it recorded, in the order
// given, each Note and all. Its --json is a hostJSON, in host show's shape,
// for koko host add <host> and for one Host read on stdin as an object, and
// a []hostJSON for an array on stdin, so what comes out mirrors what went
// in.
type addedHosts struct {
	hosts []hostJSON
	// one says the Hosts came as one: an argument or a JSON object.
	one bool
}

func (a addedHosts) MarshalJSONTo(enc *jsontext.Encoder) error {
	if a.one {
		return json.MarshalEncode(enc, a.hosts[0])
	}
	return json.MarshalEncode(enc, a.hosts)
}

// jsonShape is what koko skill documents: one Host, or an array of them for
// an array on stdin.
func (addedHosts) jsonShape() string {
	h := shape(reflect.TypeFor[hostJSON]())
	return h + " | [" + h + "]"
}

func (a addedHosts) Human(w io.Writer) error {
	for _, h := range a.hosts {
		if _, err := fmt.Fprintf(w, "Added Host %s (%s) to Space %s.\n", h.Name, h.ID, h.Space); err != nil {
			return err
		}
	}
	return nil
}

// hostAdd is koko host add <host> key=value..., or without <host> the Hosts
// JSON on stdin holds (hostAddStdin).
func hostAdd(ctx context.Context, in *Input) (addedHosts, error) {
	if in.Arg("host") == "" {
		return hostAddStdin(ctx, in)
	}
	p, err := readHostPairs(in, "add")
	if err != nil {
		return addedHosts{}, err
	}
	switch {
	case p.address == nil:
		return addedHosts{}, usagef("koko host add needs the Host's address: give address=<ip-or-dns-name>")
	case p.account == nil:
		return addedHosts{}, usagef("koko host add needs the Host Account koko logs in as, which has no default: give account=<user>")
	}
	note, err := readNote(in)
	if err != nil {
		return addedHosts{}, err
	}
	return recordHosts(ctx, in, []api.NewHost{{
		Name:       in.Arg("host"),
		Space:      p.space,
		Address:    *p.address,
		Port:       p.port,
		Account:    *p.account,
		Attributes: p.attributes,
		Note:       note,
	}}, true)
}

// recordHosts records hosts in one host.add call, all or none, and returns
// what host add prints of them.
func recordHosts(ctx context.Context, in *Input, hosts []api.NewHost, one bool) (addedHosts, error) {
	resp, err := in.Call(ctx, api.HostAdd, api.HostAddRequest{Hosts: hosts})
	if err != nil {
		return addedHosts{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	if len(resp.Hosts) != len(hosts) {
		return addedHosts{}, fmt.Errorf("host.add answered %d Hosts for %d", len(resp.Hosts), len(hosts))
	}
	added := addedHosts{hosts: make([]hostJSON, 0, len(hosts)), one: one}
	for _, h := range resp.Hosts {
		added.hosts = append(added.hosts, hostFromAPI(h))
	}
	return added, nil
}

// hostList is what host ls prints: every Host, as a bare array.
type hostList []hostJSON

func (l hostList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No Hosts.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "NAME\tSPACE\tSTATE\tADDRESS\tPORT\tACCOUNT\tID")
	for _, h := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", h.Name, h.Space, h.State, h.Address, h.Port, h.Account, h.ID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, h := range l {
		if note, ok := h.note(); ok {
			if _, err := fmt.Fprintf(w, "\nNote of %s:\n%s", h.Name, withNewline(note)); err != nil {
				return err
			}
		}
	}
	return nil
}

func hostLs(ctx context.Context, in *Input) (hostList, error) {
	filters, err := readFilters(in)
	if err != nil {
		return nil, err
	}
	sort, err := readSort(in)
	if err != nil {
		return nil, err
	}
	withNote := in.Bool("with-note")
	resp, err := in.Call(ctx, api.HostList, api.HostListRequest{Filters: filters, Sort: sort, WithNote: withNote, All: in.Bool("all")})
	if err != nil {
		return nil, err
	}
	l := make(hostList, 0, len(resp.Hosts))
	for _, h := range resp.Hosts {
		j := hostFromAPI(h)
		if !withNote {
			j.Note = nil
		}
		l = append(l, j)
	}
	return l, nil
}

// shownHost is what host show prints: the Host with its Attributes and
// Note, and, for people only, its Services. Its --json is the Host as host
// ls prints one, which host add reads back.
type shownHost struct {
	hostJSON
	services []serviceJSON
}

func (h shownHost) Human(w io.Writer) error {
	var b strings.Builder
	hostKey := "none"
	if h.HostKey != nil {
		hostKey = *h.HostKey
	}
	fmt.Fprintf(&b, "Host %s (%s)\nSpace:       %s\nState:       %s\nAddress:     %s\nPort:        %d\nAccount:     %s\nHost key:    %s\nCreated:     %s\nUpdated:     %s\n",
		h.Name, h.ID, h.Space, h.State, h.Address, h.Port, h.Account, hostKey, h.CreatedAt, h.UpdatedAt)
	writeAttributes(&b, h.Attributes)
	writeServices(&b, h.services)
	if note, ok := h.note(); ok {
		b.WriteString("Note:\n" + withNewline(note))
	} else {
		b.WriteString("Note:        none\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeAttributes writes a record's Attributes for people, as a show
// command prints them: one per line, by key, or none.
func writeAttributes(b *strings.Builder, attrs map[string]record.Value) {
	if len(attrs) == 0 {
		b.WriteString("Attributes:  none\n")
		return
	}
	b.WriteString("Attributes:\n")
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, k := range slices.Sorted(maps.Keys(attrs)) {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", k, attrs[k])
	}
	_ = tw.Flush() // a strings.Builder never fails a write
}

// withNewline ends s with a line ending, for text printed on lines of its
// own.
func withNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func hostShow(ctx context.Context, in *Input) (shownHost, error) {
	resp, err := in.Call(ctx, api.HostShow, api.HostShowRequest{Host: in.Arg("host")})
	if err != nil {
		return shownHost{}, err
	}
	h := shownHost{hostJSON: hostFromAPI(resp.Host)}
	for _, s := range resp.Services {
		h.services = append(h.services, serviceFromAPI(s))
	}
	return h, nil
}

// changedHost is what host set prints: the Host as it left it.
type changedHost hostJSON

func (h changedHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Changed Host %s (%s).\n", h.Name, h.ID)
	return err
}

func hostSet(ctx context.Context, in *Input) (changedHost, error) {
	p, err := readHostPairs(in, "set")
	if err != nil {
		return changedHost{}, err
	}
	for _, k := range in.Unset() {
		switch {
		case k == "note", k == "port":
		case record.Reserved(k):
			return changedHost{}, hostFieldRefusal("--unset "+k, k, "set")
		default:
			if err := record.CheckKey(k); err != nil {
				return changedHost{}, usagef("--unset %s: %v", k, err)
			}
		}
	}
	note, err := readNote(in)
	if err != nil {
		return changedHost{}, err
	}
	if note != nil && slices.Contains(in.Unset(), "note") {
		return changedHost{}, usagef("--note-file and --unset note are both given")
	}
	if len(in.Pairs()) == 0 && len(in.Unset()) == 0 && note == nil {
		return changedHost{}, usagef("name what to change: koko host set <host> key=value... [--unset key]... [--note-file <file>]")
	}
	resp, err := in.Call(ctx, api.HostSet, api.HostSetRequest{
		Host:       in.Arg("host"),
		Address:    p.address,
		Port:       p.port,
		Account:    p.account,
		Attributes: p.attributes,
		Note:       note,
		Unset:      in.Unset(),
	})
	if err != nil {
		return changedHost{}, err
	}
	return changedHost(hostFromAPI(resp.Host)), nil
}

// renamedHost is what host rename prints: the Host under its new name.
type renamedHost struct {
	hostJSON
	// from is the name it was called by, for people only.
	from string
}

func (h renamedHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Renamed Host %s to %s (%s).\n", h.from, h.Name, h.ID)
	return err
}

func hostRename(ctx context.Context, in *Input) (renamedHost, error) {
	resp, err := in.Call(ctx, api.HostRename, api.HostRenameRequest{Host: in.Arg("host"), NewName: in.Arg("new-name")})
	if err != nil {
		return renamedHost{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	return renamedHost{hostJSON: hostFromAPI(resp.Host), from: in.Arg("host")}, nil
}

// movedHost is what host move prints: the Host in its new Space.
type movedHost hostJSON

func (h movedHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Moved Host %s (%s) to Space %s.\n", h.Name, h.ID, h.Space)
	return err
}

func hostMove(ctx context.Context, in *Input) (movedHost, error) {
	resp, err := in.Call(ctx, api.HostMove, api.HostMoveRequest{Host: in.Arg("host"), Space: in.Arg("space")})
	if err != nil {
		return movedHost{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	return movedHost(hostFromAPI(resp.Host)), nil
}

// retiredHost is what host retire prints: the Host as it left it, Retired,
// Note and all.
type retiredHost hostJSON

func (h retiredHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Retired Host %s (%s); name it by its id from now on.\n", h.Name, h.ID)
	return err
}

func hostRetire(ctx context.Context, in *Input) (retiredHost, error) {
	resp, err := in.Call(ctx, api.HostRetire, api.HostRetireRequest{Host: in.Arg("host")})
	if err != nil {
		return retiredHost{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	return retiredHost(hostFromAPI(resp.Host)), nil
}

// removedHost is what host rm prints: the Host it deleted, as it was, Note
// and all, so a script keeps what is gone.
type removedHost hostJSON

func (h removedHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Removed Host %s (%s).\n", h.Name, h.ID)
	return err
}

func hostRm(ctx context.Context, in *Input) (removedHost, error) {
	resp, err := in.Call(ctx, api.HostRemove, api.HostRemoveRequest{Host: in.Arg("host")})
	if err != nil {
		return removedHost{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	return removedHost(hostFromAPI(resp.Host)), nil
}
