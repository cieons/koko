package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

var attrCommands = []Command{
	{
		Noun:    "attr",
		Verb:    "ls",
		Summary: "List the Preset Attributes with their types, and every other Attribute key in use on what you can see",
		Examples: []string{
			"koko attr ls",
			"koko attr ls --json",
		},
		Tier: Read,
		Who:  Anyone,
		Op:   api.AttrList,
		Run:  action(attrLs),
	},
}

// attrJSON is an Attribute key in the CLI's own --json, as attr ls prints
// it.
type attrJSON struct {
	Key string `json:"key"`
	// Type is text, number, date or enum.
	Type string `json:"type"`
	// Values are an enum's values, in the order they sort in, or null for
	// the other types: nil here.
	Values *[]string `json:"values"`
	// Min is the least value of a number that has one, such as price's 0,
	// or null.
	Min *record.Value `json:"min"`
	// Preset tells whether the key is a Preset Attribute's.
	Preset bool `json:"preset"`
	// InUse tells whether a Host or a Service you can see has the key.
	InUse bool `json:"in_use"`
}

var attrMappings = []mapping{
	{api: reflect.TypeFor[api.AttrListResponse]()},
	{api: reflect.TypeFor[api.AttrKey](), cli: reflect.TypeFor[attrJSON]()},
	// An Attribute value prints in the JSON the record gives it (package
	// record), on the API and in the CLI alike: the spec fixes both.
	{api: reflect.TypeFor[record.Value](), cli: reflect.TypeFor[record.Value]()},
}

// attrList is what attr ls prints: every Preset, in the order of the Preset
// table, then every other key in use, by key, as a bare array.
type attrList []attrJSON

// maxListedValues is the most values of an enum that attr ls lists for
// people; for more, such as the ISO 4217 codes, it names the first and last.
const maxListedValues = 10

func (l attrList) Human(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "KEY\tTYPE\tPRESET\tIN USE")
	for _, a := range l {
		typ := a.Type
		if a.Min != nil {
			typ += ", at least " + a.Min.String()
		}
		if a.Values != nil {
			switch values := *a.Values; {
			case len(values) > maxListedValues:
				typ = fmt.Sprintf("enum: %d values, %s to %s", len(values), values[0], values[len(values)-1])
			case len(values) > 0:
				typ = "enum: " + strings.Join(values, ", ")
			}
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Key, typ, yesNo(a.Preset), yesNo(a.InUse))
	}
	return tw.Flush()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func attrLs(ctx context.Context, in *Input) (attrList, error) {
	resp, err := in.Call(ctx, api.AttrList, api.AttrListRequest{})
	if err != nil {
		return nil, err
	}
	l := make(attrList, 0, len(resp.Attributes))
	for _, a := range resp.Attributes {
		j := attrJSON{Key: a.Key, Type: a.Type, Min: a.Min, Preset: a.Preset, InUse: a.InUse}
		if len(a.Values) > 0 {
			j.Values = &a.Values
		}
		l = append(l, j)
	}
	return l, nil
}
