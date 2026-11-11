package cli

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
)

// whereFlag is --where <term>, repeatable, which host set and host move
// take in place of naming Hosts: every Host host ls lists with the same
// terms. host retire and host rm take names only, so that an irreversible
// command acts on what its command line names (ADR 0050).
var whereFlag = Flag{
	Name: "where", Value: "term", Repeatable: true,
	Summary: "Act on every Host host ls lists with this filter term, such as region=hk, in place of naming Hosts; repeatable",
}

// several is what a write of records prints: for a command line that
// named one, the record alone, as before batches; for several, or for
// --where, an array of them in the order named (ADR 0050).
type several[R Result] struct {
	items []R
	// one says the command line named one record.
	one bool
}

func (s several[R]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if s.one {
		return json.MarshalEncode(enc, s.items[0])
	}
	return json.MarshalEncode(enc, s.items)
}

// jsonShape is what koko skill documents: one record, or an array of them.
func (several[R]) jsonShape() string {
	r := shape(reflect.TypeFor[R]())
	return r + " | [" + r + "]"
}

func (s several[R]) Human(w io.Writer) error {
	for _, it := range s.items {
		if err := it.Human(w); err != nil {
			return err
		}
	}
	return nil
}

// targets are the records a write acts on, as its request names them.
type targets struct {
	refs []string
	// one says the command line named one record, so the write prints it
	// alone.
	one bool
}

// form returns the request's two ways of naming the records: one alone,
// the form a koko server from before batches takes, or several, a batch.
func (t targets) form() (one string, several []string) {
	if len(t.refs) == 1 {
		return t.refs[0], nil
	}
	return "", t.refs
}

// hostTargets returns the Hosts koko host <verb> acts on: names, as typed,
// or with --where the ids of every Host host ls lists with its terms, read
// with one host.list, so the batch is what matched when it ran. For people,
// it says on stderr how many matched. It refuses before any call names and
// --where together, and neither, and a term host ls would refuse; and
// fails with unknown_host when nothing matches.
func hostTargets(ctx context.Context, in *Input, names []string) (targets, error) {
	where := in.Repeated(whereFlag.Name)
	verb := in.line.cmd.Verb
	switch {
	case len(where) > 0 && len(names) > 0:
		return targets{}, usagef("name Hosts or give --where, not both: koko host %s takes one way of choosing its Hosts", verb)
	case len(where) == 0 && len(names) == 0:
		if in.line.cmd.flag(whereFlag.Name) != nil {
			return targets{}, usagef("koko host %s needs <host>... or --where <term>", verb)
		}
		return targets{}, usagef("koko host %s needs <host>...", verb)
	case len(where) == 0:
		return targets{refs: names, one: len(names) == 1}, nil
	}
	var filters []api.Filter
	for _, term := range where {
		f, err := readTerm(in, api.HostListFields, term)
		if err != nil {
			return targets{}, usagef("--where %s: %v", term, err)
		}
		filters = append(filters, f)
	}
	resp, err := in.Call(ctx, api.HostList, api.HostListRequest{Filters: filters})
	if err != nil {
		return targets{}, err
	}
	terms := "--where " + strings.Join(where, " --where ")
	if len(resp.Hosts) == 0 {
		return targets{}, api.CodeUnknownHost.Errorf("no Host you can see matches %s, so nothing was changed", terms)
	}
	if !in.line.json {
		noun := "Hosts"
		if len(resp.Hosts) == 1 {
			noun = "Host"
		}
		_, _ = fmt.Fprintf(in.inv.Stderr, "%s matched %d %s.\n", terms, len(resp.Hosts), noun)
	}
	t := targets{refs: make([]string, 0, len(resp.Hosts))}
	for _, h := range resp.Hosts {
		t.refs = append(t.refs, h.ID)
	}
	return t, nil
}

// hostResult is what a write of the Hosts t names prints, from its answer:
// one, the one-record form's Host, or batch, each Host of a batch, made
// into the command's type by as.
func hostResult[R Result](t targets, one api.Host, batch []api.Host, as func(hostJSON) R) (several[R], error) {
	hosts := batch
	if len(t.refs) == 1 {
		hosts = []api.Host{one}
	}
	if len(hosts) != len(t.refs) {
		return several[R]{}, fmt.Errorf("the koko server answered %d Hosts for %d", len(hosts), len(t.refs))
	}
	s := several[R]{items: make([]R, 0, len(hosts)), one: t.one}
	for _, h := range hosts {
		s.items = append(s.items, as(hostFromAPI(h)))
	}
	return s, nil
}
