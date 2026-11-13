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
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/record"
)

// noteFileFlag is --note-file <file>, or --note-file - for stdin: the Note,
// which never goes through an argument.
var noteFileFlag = Flag{Name: "note-file", Value: "file", Summary: "Set the Note from a file, or from stdin with -"}

var hostCommands = []Command{
	{
		Noun:    "host",
		Verb:    "add",
		Summary: "Record a Host: address= and account= are required, space= defaults to your Personal Space, and relay= names a Managed Host to reach it through, which may have a Relay of its own, at most 4 deep; without <host>, record the Hosts JSON on stdin holds, in the shape host ls --json prints",
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
		Summary: "Change the fields, Attributes and Note of the Hosts you name, or that --where matches, and only those, all or none; relay= names a Managed Host to reach them through, which may have a Relay of its own, at most 4 deep",
		Args:    []Arg{{Name: "host", Optional: true, Many: true}},
		Flags:   []Flag{noteFileFlag, whereFlag},
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
		Summary: "Move the Hosts you name, or that --where matches, to the Space --to names, all or none; you must be able to act in both. koko host move <host> <space> moves one",
		Args:    []Arg{{Name: "host", Optional: true, Many: true}},
		Flags: []Flag{
			{Name: "to", Value: "space", Summary: "The Space to move the Hosts to"},
			whereFlag,
		},
		Tier: Reversible,
		Op:   api.HostMove,
		Run:  action(hostMove),
	},
	{
		Noun:    "host",
		Verb:    "retire",
		Summary: "Retire the Hosts you name, all or none, for Hosts that are gone: each leaves host ls and frees its name, and keeps its record, which its id names from then on",
		Args:    []Arg{{Name: "host", Many: true}},
		Tier:    Irreversible,
		Op:      api.HostRetire,
		Run:     action(hostRetire),
	},
	{
		Noun:    "host",
		Verb:    "rm",
		Summary: "Delete the Hosts you name, all or none, each never Managed, with its Attributes, Note and Services",
		Args:    []Arg{{Name: "host", Many: true}},
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
	// Relay is the Host koko passes through to reach this one, or null for
	// none.
	Relay *relayJSON `json:"relay"`
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

// relayJSON is a Host's Relay in the CLI's own --json (ADR 0035): its id
// and its name, as a Host shows its own, both null for a Relay you cannot
// see.
type relayJSON struct {
	ID   *string `json:"id"`
	Name *string `json:"name"`
}

// relayFromAPI maps a Host's Relay from the API, nil for none.
func relayFromAPI(r *api.HostRelay) *relayJSON {
	if r == nil {
		return nil
	}
	return &relayJSON{ID: r.ID, Name: r.Name}
}

// String is the Relay for people: its name, - for none, or (unseen) for
// one you cannot see.
func (r *relayJSON) String() string {
	switch {
	case r == nil:
		return "-"
	case r.Name == nil:
		return "(unseen)"
	}
	return *r.Name
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
		Relay:      relayFromAPI(h.Relay),
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
		"path": "host show prints a Host's path for people only: its --json is the Host as host ls prints one, " +
			"whose chain an agent follows by each relay.id (ADR 0055)",
	}},
	{api: reflect.TypeFor[api.HostSetResponse]()},
	{api: reflect.TypeFor[api.HostRenameResponse]()},
	{api: reflect.TypeFor[api.HostMoveResponse]()},
	{api: reflect.TypeFor[api.HostRetireResponse]()},
	{api: reflect.TypeFor[api.HostRemoveResponse]()},
	{api: reflect.TypeFor[api.Host](), cli: reflect.TypeFor[hostJSON]()},
	{api: reflect.TypeFor[api.HostRelay](), cli: reflect.TypeFor[relayJSON]()},
}

// hostPairs are the key=value words of host add or host set, sorted into
// the Host's fields and its Attributes.
type hostPairs struct {
	address, account, space, relay *string
	port                           *int
	attributes                     map[string]record.Value
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
		case pr.Key == "relay":
			if err := checkHostRef(pr.Value); err != nil {
				return hostPairs{}, usagef("relay=: %v", err)
			}
			p.relay = new(pr.Value)
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

// checkHostRef reports why ref names no Host, by name or id, before any
// call, or nil when it could: whether a Host has it is the koko server's to
// say.
func checkHostRef(ref string) error {
	if strings.HasPrefix(ref, "host_") {
		return nil
	}
	if err := name.Check(ref); err != nil {
		return fmt.Errorf("%q is neither a Host name nor a Host id: %w", ref, err)
	}
	return nil
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
		Relay:      p.relay,
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
	_, _ = fmt.Fprintln(tw, "NAME\tSPACE\tSTATE\tADDRESS\tPORT\tACCOUNT\tRELAY\tID")
	for _, h := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n", h.Name, h.Space, h.State, h.Address, h.Port, h.Account, h.Relay, h.ID)
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
// Note, and, for people only, its path and its Services. Its --json is the
// Host as host ls prints one, which host add reads back.
type shownHost struct {
	hostJSON
	// through is the path's Relays before the Host's own, in dial order,
	// for people: a name, or one you cannot see.
	through  []string
	services []serviceJSON
}

func (h shownHost) Human(w io.Writer) error {
	var b strings.Builder
	hostKey := "none"
	if h.HostKey != nil {
		hostKey = *h.HostKey
	}
	relay := "none"
	switch r := h.Relay; {
	case r != nil && r.Name == nil:
		relay = "one you cannot see"
	case r != nil:
		relay = fmt.Sprintf("%s (%s)", *r.Name, *r.ID)
		if len(h.through) > 0 {
			relay += ", through " + strings.Join(h.through, ", ")
		}
	}
	fmt.Fprintf(&b, "Host %s (%s)\nSpace:       %s\nState:       %s\nAddress:     %s\nPort:        %d\nAccount:     %s\nRelay:       %s\nHost key:    %s\nCreated:     %s\nUpdated:     %s\n",
		h.Name, h.ID, h.Space, h.State, h.Address, h.Port, h.Account, relay, hostKey, h.CreatedAt, h.UpdatedAt)
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
	// An older koko server answers no path, and its Relay has none of its
	// own.
	if len(resp.Path) > 1 {
		for _, r := range resp.Path[:len(resp.Path)-1] {
			name := "one you cannot see"
			if r.Name != nil {
				name = *r.Name
			}
			h.through = append(h.through, name)
		}
	}
	for _, s := range resp.Services {
		h.services = append(h.services, serviceFromAPI(s))
	}
	return h, nil
}

// changedHost is what host set prints of each Host: the Host as it left
// it.
type changedHost hostJSON

func (h changedHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Changed Host %s (%s).\n", h.Name, h.ID)
	return err
}

func hostSet(ctx context.Context, in *Input) (several[changedHost], error) {
	p, err := readHostPairs(in, "set")
	if err != nil {
		return several[changedHost]{}, err
	}
	for _, k := range in.Unset() {
		switch {
		case k == "note", k == "port", k == "relay":
		case record.Reserved(k):
			return several[changedHost]{}, hostFieldRefusal("--unset "+k, k, "set")
		default:
			if err := record.CheckKey(k); err != nil {
				return several[changedHost]{}, usagef("--unset %s: %v", k, err)
			}
		}
	}
	note, err := readNote(in)
	if err != nil {
		return several[changedHost]{}, err
	}
	if note != nil && slices.Contains(in.Unset(), "note") {
		return several[changedHost]{}, usagef("--note-file and --unset note are both given")
	}
	if len(in.Pairs()) == 0 && len(in.Unset()) == 0 && note == nil {
		return several[changedHost]{}, usagef("name what to change: koko host set <host>... key=value... [--unset key]... [--note-file <file>]")
	}
	t, err := hostTargets(ctx, in, in.Args("host"))
	if err != nil {
		return several[changedHost]{}, err
	}
	req := api.HostSetRequest{
		Address:    p.address,
		Port:       p.port,
		Account:    p.account,
		Relay:      p.relay,
		Attributes: p.attributes,
		Note:       note,
		Unset:      in.Unset(),
	}
	req.Host, req.Hosts = t.form()
	resp, err := in.Call(ctx, api.HostSet, req)
	if err != nil {
		return several[changedHost]{}, err
	}
	return hostResult(t, resp.Host, resp.Hosts, func(h hostJSON) changedHost { return changedHost(h) })
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

// movedHost is what host move prints of each Host: the Host in its new
// Space.
type movedHost hostJSON

func (h movedHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Moved Host %s (%s) to Space %s.\n", h.Name, h.ID, h.Space)
	return err
}

// hostMove is koko host move <host>... --to <space>, or --where in place of
// the Hosts; without --to, koko host move <host> <space> moves one Host, as
// before batches.
func hostMove(ctx context.Context, in *Input) (several[movedHost], error) {
	names, to := in.Args("host"), in.Flag("to")
	if to == "" && len(names) == 2 && len(in.Repeated(whereFlag.Name)) == 0 {
		names, to = names[:1], names[1]
	}
	if to == "" {
		return several[movedHost]{}, usagef("name the Space to move the Hosts to: koko host move <host>... --to <space>")
	}
	t, err := hostTargets(ctx, in, names)
	if err != nil {
		return several[movedHost]{}, err
	}
	req := api.HostMoveRequest{Space: to}
	req.Host, req.Hosts = t.form()
	resp, err := in.Call(ctx, api.HostMove, req)
	if err != nil {
		return several[movedHost]{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	return hostResult(t, resp.Host, resp.Hosts, func(h hostJSON) movedHost { return movedHost(h) })
}

// retiredHost is what host retire prints of each Host: the Host as it left
// it, Retired, Note and all.
type retiredHost hostJSON

func (h retiredHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Retired Host %s (%s); name it by its id from now on.\n", h.Name, h.ID)
	return err
}

func hostRetire(ctx context.Context, in *Input) (several[retiredHost], error) {
	t, err := hostTargets(ctx, in, in.Args("host"))
	if err != nil {
		return several[retiredHost]{}, err
	}
	var req api.HostRetireRequest
	req.Host, req.Hosts = t.form()
	resp, err := in.Call(ctx, api.HostRetire, req)
	if err != nil {
		return several[retiredHost]{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	return hostResult(t, resp.Host, resp.Hosts, func(h hostJSON) retiredHost { return retiredHost(h) })
}

// removedHost is what host rm prints of each Host: the Host it deleted, as
// it was, Note and all, so a script keeps what is gone.
type removedHost hostJSON

func (h removedHost) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Removed Host %s (%s).\n", h.Name, h.ID)
	return err
}

func hostRm(ctx context.Context, in *Input) (several[removedHost], error) {
	t, err := hostTargets(ctx, in, in.Args("host"))
	if err != nil {
		return several[removedHost]{}, err
	}
	var req api.HostRemoveRequest
	req.Host, req.Hosts = t.form()
	resp, err := in.Call(ctx, api.HostRemove, req)
	if err != nil {
		return several[removedHost]{}, err
	}
	in.rewriteBareNamesAfterHostChange(ctx)
	return hostResult(t, resp.Host, resp.Hosts, func(h hostJSON) removedHost { return removedHost(h) })
}
