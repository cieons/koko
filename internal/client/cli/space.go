package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
)

var spaceCommands = []Command{
	{
		Noun:      "space",
		Verb:      "add",
		Summary:   "Record a shared Space",
		Args:      []Arg{{Name: "space"}},
		Tier:      Reversible,
		OwnerOnly: true,
		Op:        api.SpaceAdd,
		Run:       action(spaceAdd),
	},
	{
		Noun:    "space",
		Verb:    "ls",
		Summary: "List the Spaces you can act in",
		Tier:    Read,
		Op:      api.SpaceList,
		Run:     action(spaceLs),
	},
}

// spaceJSON is a Space in the CLI's own --json.
type spaceJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func spaceFromAPI(s api.Space) spaceJSON {
	return spaceJSON{ID: s.ID, Name: s.Name}
}

var spaceMappings = []mapping{
	{api: reflect.TypeFor[api.SpaceAddResponse]()},
	{api: reflect.TypeFor[api.SpaceListResponse]()},
	{api: reflect.TypeFor[api.Space](), cli: reflect.TypeFor[spaceJSON]()},
}

// addedSpace is what space add prints: the Space it made.
type addedSpace spaceJSON

func (s addedSpace) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Added Space %s (%s).\n", s.Name, s.ID)
	return err
}

func spaceAdd(ctx context.Context, in *Input) (addedSpace, error) {
	resp, err := in.Call(ctx, api.SpaceAdd, api.SpaceAddRequest{Name: in.Arg("space")})
	if err != nil {
		return addedSpace{}, err
	}
	return addedSpace(spaceFromAPI(resp.Space)), nil
}

// spaceList is what space ls prints: every Space, as a bare array.
type spaceList []spaceJSON

func (l spaceList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No Spaces.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "NAME\tID")
	for _, s := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", s.Name, s.ID)
	}
	return tw.Flush()
}

func spaceLs(ctx context.Context, in *Input) (spaceList, error) {
	resp, err := in.Call(ctx, api.SpaceList, api.SpaceListRequest{})
	if err != nil {
		return nil, err
	}
	l := make(spaceList, 0, len(resp.Spaces))
	for _, s := range resp.Spaces {
		l = append(l, spaceFromAPI(s))
	}
	return l, nil
}
