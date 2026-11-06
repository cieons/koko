package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// serviceArg is <host>/<service>, the qualified name of a Service: <host> a
// Host's name or id.
var serviceArg = Arg{Name: "service", Qualifier: "host"}

var serviceCommands = []Command{
	{
		Noun:    "service",
		Verb:    "add",
		Summary: "Record a Service on a Host, with the ports it listens on, such as ports=443/tcp,51820/udp, or none",
		Args:    []Arg{serviceArg},
		Flags:   []Flag{noteFileFlag},
		Pairs:   true,
		Tier:    Reversible,
		Op:      api.ServiceAdd,
		Run:     action(serviceAdd),
	},
	{
		Noun:    "service",
		Verb:    "ls",
		Summary: "List the Services on every Host you can see that match every filter term, such as host=web1 kind=proxy",
		Flags:   []Flag{{Name: "with-note", Bool: true, Summary: "Include each Service's Note"}},
		Filters: api.ServiceListFields,
		Tier:    Read,
		Op:      api.ServiceList,
		Run:     action(serviceLs),
	},
	{
		Noun:    "service",
		Verb:    "show",
		Summary: "Show a Service with its ports, Attributes and Note",
		Args:    []Arg{serviceArg},
		Tier:    Read,
		Op:      api.ServiceShow,
		Run:     action(serviceShow),
	},
	{
		Noun:    "service",
		Verb:    "set",
		Summary: "Change the ports, Attributes and Note of a Service that you name, and only those",
		Args:    []Arg{serviceArg},
		Flags:   []Flag{noteFileFlag},
		Pairs:   true,
		Unset:   true,
		Tier:    Reversible,
		Op:      api.ServiceSet,
		Run:     action(serviceSet),
	},
	{
		Noun:    "service",
		Verb:    "rename",
		Summary: "Give a Service a new name on its Host",
		Args:    []Arg{serviceArg, {Name: "new-name"}},
		Tier:    Reversible,
		Op:      api.ServiceRename,
		Run:     action(serviceRename),
	},
	{
		Noun:    "service",
		Verb:    "rm",
		Summary: "Delete a Service, with its ports, Attributes and Note",
		Args:    []Arg{serviceArg},
		Tier:    Irreversible,
		Op:      api.ServiceRemove,
		Run:     action(serviceRm),
	},
}

// serviceJSON is a Service in the CLI's own --json.
type serviceJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Host is the name of the Host it runs on, or the Host's id once it is
	// Retired, so <host>/<service> names it.
	Host string `json:"host"`
	// Ports are the ports it listens on, each <port>/<protocol> as in
	// 443/tcp, by port and then protocol; [] for none.
	Ports []string `json:"ports"`
	// Attributes sit in their own object, typed as a Host's.
	Attributes map[string]record.Value `json:"attributes"`
	// Note is the Service's Note, or null for none, as on a Host: service
	// ls prints it only with --with-note, and without it leaves the member
	// out.
	Note      **string `json:"note,omitzero"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// serviceFromAPI maps a Service from the API, with its Note.
func serviceFromAPI(s api.Service) serviceJSON {
	j := serviceJSON{
		ID:         s.ID,
		Name:       s.Name,
		Host:       s.Host,
		Ports:      append([]string{}, s.Ports...),
		Attributes: maps.Clone(s.Attributes),
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
	}
	if j.Attributes == nil {
		j.Attributes = map[string]record.Value{}
	}
	var note *string
	if s.Note != "" {
		note = new(s.Note)
	}
	j.Note = &note
	return j
}

// qualified is the Service's name as a command takes it, <host>/<service>.
func (s serviceJSON) qualified() string {
	return s.Host + "/" + s.Name
}

var serviceMappings = []mapping{
	{api: reflect.TypeFor[api.ServiceAddResponse]()},
	{api: reflect.TypeFor[api.ServiceListResponse]()},
	{api: reflect.TypeFor[api.ServiceShowResponse]()},
	{api: reflect.TypeFor[api.ServiceSetResponse]()},
	{api: reflect.TypeFor[api.ServiceRenameResponse]()},
	{api: reflect.TypeFor[api.ServiceRemoveResponse]()},
	{api: reflect.TypeFor[api.Service](), cli: reflect.TypeFor[serviceJSON]()},
}

// servicePairs are the key=value words of service add or service set,
// sorted into the Service's ports and its Attributes.
type servicePairs struct {
	// ports is nil when ports= is not given.
	ports      []string
	attributes map[string]record.Value
}

// readServicePairs sorts the key=value words of koko service <verb> into
// the ports and the Attributes, refusing before any call a key the command
// does not take and a value that breaks the record's rules, as
// readHostPairs does for a Host.
func readServicePairs(in *Input, verb string) (servicePairs, error) {
	p := servicePairs{attributes: map[string]record.Value{}}
	for _, pr := range in.Pairs() {
		switch {
		case pr.Key == "ports":
			set, err := record.ParseServicePorts(strings.Split(pr.Value, ","))
			if err != nil {
				return servicePairs{}, usagef("ports=: %v", err)
			}
			p.ports = []string{}
			for _, port := range set {
				p.ports = append(p.ports, port.String())
			}
		case record.Reserved(pr.Key):
			return servicePairs{}, serviceFieldRefusal(pr.Key+"=", pr.Key, verb)
		default:
			if err := record.CheckKey(pr.Key); err != nil {
				return servicePairs{}, usagef("%s=: %v", pr.Key, err)
			}
			v, err := in.parseValue(record.TypeOf(pr.Key), pr.Value)
			if err != nil {
				return servicePairs{}, usagef("the value of %s=: %v", pr.Key, err)
			}
			p.attributes[pr.Key] = v
		}
	}
	return p, nil
}

// serviceFieldRefusal refuses written, which gives or clears the field key
// that koko service <verb> does not take, saying how to change it.
func serviceFieldRefusal(written, key, verb string) error {
	switch key {
	case "name":
		if verb == "add" {
			return usagef("%s: the Service's name is the argument of koko service add <host>/<service>", written)
		}
		return usagef("%s: koko service %s does not rename a Service; write koko service rename <host>/<service> <new-name>", written, verb)
	case "host":
		return usagef("%s: a Service never moves to another Host; record it on the new Host with koko service add <host>/<service>", written)
	case "space":
		return usagef("%s: a Service lives in its Host's Space; move the Host with koko host move <host> <space>", written)
	case "note":
		return usagef("%s: the Note is read from a file with --note-file <file>, or from stdin with --note-file -", written)
	}
	return usagef("%s: %s is a field koko service %s does not take", written, key, verb)
}

// addedService is what service add prints: the Service it recorded.
type addedService serviceJSON

func (s addedService) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Added Service %s (%s).\n", serviceJSON(s).qualified(), s.ID)
	return err
}

func serviceAdd(ctx context.Context, in *Input) (addedService, error) {
	p, err := readServicePairs(in, "add")
	if err != nil {
		return addedService{}, err
	}
	note, err := readNote(in)
	if err != nil {
		return addedService{}, err
	}
	hostRef, name := in.Qualified(serviceArg.Name)
	resp, err := in.Call(ctx, api.ServiceAdd, api.ServiceAddRequest{
		Host:       hostRef,
		Name:       name,
		Ports:      p.ports,
		Attributes: p.attributes,
		Note:       note,
	})
	if err != nil {
		return addedService{}, err
	}
	return addedService(serviceFromAPI(resp.Service)), nil
}

// portsText writes a Service's ports for people as ports= takes them,
// 80/tcp,443/tcp, or none for a Service that listens on none.
func portsText(ports []string, none string) string {
	if len(ports) == 0 {
		return none
	}
	return strings.Join(ports, ",")
}

// writeServices writes the Services on a Host for people, as host show
// prints them: one per line with its ports, by name, or none.
func writeServices(b *strings.Builder, services []serviceJSON) {
	if len(services) == 0 {
		b.WriteString("Services:    none\n")
		return
	}
	b.WriteString("Services:\n")
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, s := range services {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", s.Name, portsText(s.Ports, "-"))
	}
	_ = tw.Flush() // a strings.Builder never fails a write
}

// serviceList is what service ls prints: every Service, as a bare array.
type serviceList []serviceJSON

func (l serviceList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No Services.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "NAME\tPORTS\tID")
	for _, s := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", s.qualified(), portsText(s.Ports, "-"), s.ID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, s := range l {
		if note, ok := s.note(); ok {
			if _, err := fmt.Fprintf(w, "\nNote of %s:\n%s", s.qualified(), withNewline(note)); err != nil {
				return err
			}
		}
	}
	return nil
}

// note returns the Service's Note, and whether it has one.
func (s serviceJSON) note() (string, bool) {
	if s.Note == nil || *s.Note == nil {
		return "", false
	}
	return **s.Note, true
}

func serviceLs(ctx context.Context, in *Input) (serviceList, error) {
	filters, err := readFilters(in)
	if err != nil {
		return nil, err
	}
	sort, err := readSort(in)
	if err != nil {
		return nil, err
	}
	withNote := in.Bool("with-note")
	resp, err := in.Call(ctx, api.ServiceList, api.ServiceListRequest{Filters: filters, Sort: sort, WithNote: withNote})
	if err != nil {
		return nil, err
	}
	l := make(serviceList, 0, len(resp.Services))
	for _, s := range resp.Services {
		j := serviceFromAPI(s)
		if !withNote {
			j.Note = nil
		}
		l = append(l, j)
	}
	return l, nil
}

// shownService is what service show prints: the Service with its ports,
// Attributes and Note.
type shownService serviceJSON

func (s shownService) Human(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Service %s (%s)\nHost:        %s\nPorts:       %s\nCreated:     %s\nUpdated:     %s\n",
		serviceJSON(s).qualified(), s.ID, s.Host, portsText(s.Ports, "none"), s.CreatedAt, s.UpdatedAt)
	writeAttributes(&b, s.Attributes)
	if note, ok := serviceJSON(s).note(); ok {
		b.WriteString("Note:\n" + withNewline(note))
	} else {
		b.WriteString("Note:        none\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func serviceShow(ctx context.Context, in *Input) (shownService, error) {
	hostRef, name := in.Qualified(serviceArg.Name)
	resp, err := in.Call(ctx, api.ServiceShow, api.ServiceShowRequest{Host: hostRef, Name: name})
	if err != nil {
		return shownService{}, err
	}
	return shownService(serviceFromAPI(resp.Service)), nil
}

// changedService is what service set prints: the Service as it left it.
type changedService serviceJSON

func (s changedService) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Changed Service %s (%s).\n", serviceJSON(s).qualified(), s.ID)
	return err
}

func serviceSet(ctx context.Context, in *Input) (changedService, error) {
	p, err := readServicePairs(in, "set")
	if err != nil {
		return changedService{}, err
	}
	for _, k := range in.Unset() {
		switch {
		case k == "note", k == "ports":
		case record.Reserved(k):
			return changedService{}, serviceFieldRefusal("--unset "+k, k, "set")
		default:
			if err := record.CheckKey(k); err != nil {
				return changedService{}, usagef("--unset %s: %v", k, err)
			}
		}
	}
	note, err := readNote(in)
	if err != nil {
		return changedService{}, err
	}
	if note != nil && slices.Contains(in.Unset(), "note") {
		return changedService{}, usagef("--note-file and --unset note are both given")
	}
	if len(in.Pairs()) == 0 && len(in.Unset()) == 0 && note == nil {
		return changedService{}, usagef("name what to change: koko service set <host>/<service> key=value... [--unset key]... [--note-file <file>]")
	}
	hostRef, name := in.Qualified(serviceArg.Name)
	resp, err := in.Call(ctx, api.ServiceSet, api.ServiceSetRequest{
		Host:       hostRef,
		Name:       name,
		Ports:      p.ports,
		Attributes: p.attributes,
		Note:       note,
		Unset:      in.Unset(),
	})
	if err != nil {
		return changedService{}, err
	}
	return changedService(serviceFromAPI(resp.Service)), nil
}

// renamedService is what service rename prints: the Service under its new
// name.
type renamedService struct {
	serviceJSON
	// from is the name it was called by, for people only.
	from string
}

func (s renamedService) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Renamed Service %s to %s (%s).\n", s.from, s.qualified(), s.ID)
	return err
}

func serviceRename(ctx context.Context, in *Input) (renamedService, error) {
	newName := in.Arg("new-name")
	if strings.Contains(newName, "/") {
		return renamedService{}, usagef("the new name %q holds /: a Service is renamed on its own Host, and never moves to another", newName)
	}
	hostRef, name := in.Qualified(serviceArg.Name)
	resp, err := in.Call(ctx, api.ServiceRename, api.ServiceRenameRequest{Host: hostRef, Name: name, NewName: newName})
	if err != nil {
		return renamedService{}, err
	}
	return renamedService{serviceJSON: serviceFromAPI(resp.Service), from: in.Arg(serviceArg.Name)}, nil
}

// removedService is what service rm prints: the Service it deleted, as it
// was, Note and all, so a script keeps what is gone.
type removedService serviceJSON

func (s removedService) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Removed Service %s (%s).\n", serviceJSON(s).qualified(), s.ID)
	return err
}

func serviceRm(ctx context.Context, in *Input) (removedService, error) {
	hostRef, name := in.Qualified(serviceArg.Name)
	resp, err := in.Call(ctx, api.ServiceRemove, api.ServiceRemoveRequest{Host: hostRef, Name: name})
	if err != nil {
		return removedService{}, err
	}
	return removedService(serviceFromAPI(resp.Service)), nil
}
