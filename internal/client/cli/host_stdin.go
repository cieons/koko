package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/record"
)

// hostInput is one Host that koko host add reads on stdin, in the shape host
// ls --json prints (hostJSON), so that ls --json output feeds straight back
// in. The fields only koko sets are taken and ignored, whatever they hold.
type hostInput struct {
	Name       string                  `json:"name"`
	Space      *string                 `json:"space"`
	Address    string                  `json:"address"`
	Port       *int                    `json:"port"`
	Account    string                  `json:"account"`
	Attributes map[string]record.Value `json:"attributes"`
	Note       *string                 `json:"note"`

	ID        jsontext.Value `json:"id"`
	State     jsontext.Value `json:"state"`
	HostKey   jsontext.Value `json:"host_key"`
	CreatedAt jsontext.Value `json:"created_at"`
	UpdatedAt jsontext.Value `json:"updated_at"`
}

// hostInputFields are the members a Host on stdin may hold: hostInput's.
var hostInputFields = func() []string {
	var names []string
	for f := range reflect.TypeFor[hostInput]().Fields() {
		if name, ok := jsonName(f); ok {
			names = append(names, name)
		}
	}
	return names
}()

// hostAddStdin is koko host add without a name: it records the Hosts JSON on
// stdin holds, one as an object or several as an array, all or none. Every
// Host meets the rules readHostPairs checks, before any call; a batch with
// Hosts at fault is refused listing each of them (api.HostBatchError).
func hostAddStdin(ctx context.Context, in *Input) (addedHosts, error) {
	switch {
	case len(in.Pairs()) > 0:
		return addedHosts{}, usagef("%s=: key=value words need the Host's name, koko host add <host> key=value...; a Host as JSON on stdin gives its fields as members", in.Pairs()[0].Key)
	case in.Flag(noteFileFlag.Name) != "":
		return addedHosts{}, usagef(`--note-file needs the Host's name, koko host add <host> --note-file <file>; a Host as JSON on stdin gives its Note as "note"`)
	case in.terminal() != nil:
		return addedHosts{}, usagef("koko host add without a Host's name reads Hosts as JSON on stdin, which is a terminal: pipe them in, or give koko host add <host> key=value...")
	}
	b, err := in.readStdin(api.MaxRequestBody)
	switch {
	case err != nil:
		return addedHosts{}, usagef("%v", err)
	case len(b) > api.MaxRequestBody:
		return addedHosts{}, usagef("stdin holds more than the %d MiB one call carries: add the Hosts in smaller batches", api.MaxRequestBody>>20)
	case len(bytes.TrimSpace(b)) == 0:
		return addedHosts{}, usagef("stdin is empty: koko host add without a Host's name reads Hosts as JSON on stdin, one as an object or several as an array, as koko host ls --json prints them")
	}
	var top jsontext.Value
	if err := json.Unmarshal(b, &top); err != nil {
		return addedHosts{}, usagef("stdin is not JSON: %v", err)
	}
	items, one := []jsontext.Value{top}, true
	if top.Kind() == '[' {
		if err := json.Unmarshal(top, &items); err != nil {
			return addedHosts{}, usagef("stdin is not JSON: %v", err)
		}
		if len(items) == 0 {
			return addedHosts{}, usagef("stdin holds an empty array: give at least one Host")
		}
		one = false
	}
	hosts := make([]api.NewHost, 0, len(items))
	var faults []api.ItemError
	for i, item := range items {
		n, err := readHostJSON(item)
		if err != nil {
			faults = append(faults, api.ItemError{Index: i, Code: codeInvalidUsage, Message: err.Error()})
			continue
		}
		hosts = append(hosts, n)
	}
	switch {
	case len(faults) == 0:
		return recordHosts(ctx, in, hosts, one)
	case len(items) == 1:
		return addedHosts{}, &usageError{message: faults[0].Message}
	}
	e := api.HostBatchError(len(items), faults)
	return addedHosts{}, &usageError{message: e.Message, items: e.Items}
}

// readHostJSON reads one Host that stdin holds, refusing what koko host add
// <host> key=value... refuses before any call, and a member hostInput does
// not have, by name. The address and the Attributes come back in the form
// koko stores. The Host's name, when it meets the name rule, starts each
// refusal, as the koko server's own refusals start.
func readHostJSON(item jsontext.Value) (api.NewHost, error) {
	if item.Kind() != '{' {
		return api.NewHost{}, fmt.Errorf("a Host is a JSON object, not %s", kindName(item.Kind()))
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(item, &members); err != nil {
		return api.NewHost{}, err
	}
	var hostName string
	if json.Unmarshal(members["name"], &hostName) != nil || name.Check(hostName) != nil {
		hostName = ""
	}
	fault := func(format string, args ...any) error {
		if hostName == "" {
			return fmt.Errorf(format, args...)
		}
		return fmt.Errorf("the Host %s: %s", hostName, fmt.Sprintf(format, args...))
	}

	var unknown []string
	for _, k := range slices.Sorted(maps.Keys(members)) {
		if !slices.Contains(hostInputFields, k) {
			unknown = append(unknown, strconv.Quote(k))
		}
	}
	if len(unknown) > 0 {
		return api.NewHost{}, fault(`unknown field %s: koko host add reads the fields koko host ls --json prints, with the Attributes in "attributes"`,
			strings.Join(unknown, ", "))
	}
	var h hostInput
	if err := json.Unmarshal(item, &h); err != nil {
		return api.NewHost{}, fault("%v", decodeFault(err))
	}
	switch {
	case h.Name == "":
		return api.NewHost{}, fault(`"name" is missing: every Host needs its name, its SSH Name`)
	case h.Address == "":
		return api.NewHost{}, fault(`"address" is missing: every Host needs an IP address or a DNS name`)
	case h.Account == "":
		return api.NewHost{}, fault(`"account" is missing: every Host needs the Host Account koko logs in as, which has no default`)
	case h.Space != nil && *h.Space == "":
		return api.NewHost{}, fault(`"space" is empty: leave it out for your Personal Space`)
	}
	address, err := record.Address(h.Address)
	if err != nil {
		return api.NewHost{}, fault("%v", err)
	}
	if h.Port != nil {
		if err := record.CheckPort(*h.Port); err != nil {
			return api.NewHost{}, fault("%v", err)
		}
	}
	if err := record.CheckAccount(h.Account); err != nil {
		return api.NewHost{}, fault("%v", err)
	}
	attributes := make(map[string]record.Value, len(h.Attributes))
	for _, k := range slices.Sorted(maps.Keys(h.Attributes)) {
		if err := record.CheckKey(k); err != nil {
			return api.NewHost{}, fault("%v", err)
		}
		v, err := record.TypeOf(k).Check(h.Attributes[k])
		if err != nil {
			return api.NewHost{}, fault("the Attribute %s: %v", k, err)
		}
		attributes[k] = v
	}
	if h.Note != nil {
		if err := record.CheckNote(*h.Note); err != nil {
			return api.NewHost{}, fault("%v", err)
		}
	}
	return api.NewHost{
		Name: h.Name, Space: h.Space, Address: address, Port: h.Port, Account: h.Account,
		Attributes: attributes, Note: h.Note,
	}, nil
}

// takes says what each kind of Go value in hostInput takes in JSON.
var takes = map[reflect.Kind]string{
	reflect.Int:    "a whole number",
	reflect.String: "a string",
	reflect.Map:    "an object",
}

// decodeFault says why a Host on stdin did not decode: a member, named by
// its path, of a JSON type its field does not take.
func decodeFault(err error) error {
	se, ok := errors.AsType[*json.SemanticError](err)
	if !ok || se.JSONPointer == "" || se.GoType == nil {
		return err
	}
	at := strings.ReplaceAll(strings.TrimPrefix(string(se.JSONPointer), "/"), "/", ".")
	if want, ok := takes[se.GoType.Kind()]; ok {
		return fmt.Errorf("%q takes %s", at, want)
	}
	return fmt.Errorf("%q: %w", at, cmp.Or(se.Err, err))
}

// kindName names a kind of JSON value for people: "an array", "a number".
func kindName(k jsontext.Kind) string {
	switch k {
	case '[':
		return "an array"
	case '"':
		return "a string"
	case '0':
		return "a number"
	case 't', 'f':
		return "a boolean"
	case 'n':
		return "null"
	}
	return "an object"
}
