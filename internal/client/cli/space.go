package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
)

var spaceCommands = []Command{
	{
		Noun:    "space",
		Verb:    "add",
		Summary: "Record a shared Space",
		Args:    []Arg{{Name: "space"}},
		Examples: []string{
			"koko space add ops",
		},
		Tier: Reversible,
		Who:  OwnerOrAdministrator,
		Op:   api.SpaceAdd,
		Run:  action(spaceAdd),
	},
	{
		Noun:    "space",
		Verb:    "ls",
		Summary: "List the Spaces you can act in",
		Examples: []string{
			"koko space ls",
		},
		Tier: Read,
		Who:  Anyone,
		Op:   api.SpaceList,
		Run:  action(spaceLs),
	},
	{
		Noun:    "space",
		Verb:    "show",
		Summary: "Show a Space with its Members and how many Hosts and Secrets it holds",
		Args:    []Arg{{Name: "space"}},
		Examples: []string{
			"koko space show ops",
		},
		Tier: Read,
		Who:  Anyone,
		Op:   api.SpaceShow,
		Run:  action(spaceShow),
	},
	{
		Noun:    "space",
		Verb:    "rename",
		Summary: "Give a shared Space a new name",
		Args:    []Arg{{Name: "space"}, {Name: "new-name"}},
		Examples: []string{
			"koko space rename ops platform",
		},
		Tier: Reversible,
		Who:  OwnerOrAdministrator,
		Op:   api.SpaceRename,
		Run:  action(spaceRename),
	},
	{
		Noun:    "space",
		Verb:    "rm",
		Summary: "Remove a shared Space that holds no Host or Secret",
		Args:    []Arg{{Name: "space"}},
		Examples: []string{
			"koko space rm ops",
		},
		Tier: Irreversible,
		Who:  OwnerOrAdministrator,
		Op:   api.SpaceRemove,
		Run:  action(spaceRm),
	},
	{
		Noun:    "space member",
		Verb:    "add",
		Summary: "Make Users Members of a Space, all or none",
		Args:    []Arg{{Name: "space"}, {Name: "user", Many: true}},
		Examples: []string{
			"koko space member add ops alice bob",
		},
		Tier: Reversible,
		Who:  OwnerOrAdministrator,
		Op:   api.SpaceMemberAdd,
		Run:  action(spaceMemberAdd),
	},
	{
		Noun:    "space member",
		Verb:    "rm",
		Summary: "Take Users out of a Space, all or none",
		Args:    []Arg{{Name: "space"}, {Name: "user", Many: true}},
		Examples: []string{
			"koko space member rm ops alice",
		},
		Tier: Reversible,
		Who:  OwnerOrAdministrator,
		Op:   api.SpaceMemberRemove,
		Run:  action(spaceMemberRm),
	},
}

// spaceJSON is a Space in the CLI's own --json.
type spaceJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Personal is set for a User's Personal Space.
	Personal bool `json:"personal"`
}

func spaceFromAPI(s api.Space) spaceJSON {
	return spaceJSON{ID: s.ID, Name: s.Name, Personal: s.Personal}
}

// spaceDetailJSON is a Space with its Members and counts, as space show
// prints it.
type spaceDetailJSON struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Personal bool              `json:"personal"`
	Members  []spaceMemberJSON `json:"members"`
	Counts   spaceCountsJSON   `json:"counts"`
}

// spaceMemberJSON is a Member of a Space: the User's id and name.
type spaceMemberJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// spaceCountsJSON is how many Hosts and Secrets a Space holds: Hosts that
// are not Retired, and Retired ones apart.
type spaceCountsJSON struct {
	Hosts        int `json:"hosts"`
	RetiredHosts int `json:"retired_hosts"`
	Secrets      int `json:"secrets"`
}

func spaceDetailFromAPI(d api.SpaceDetail) spaceDetailJSON {
	members := make([]spaceMemberJSON, 0, len(d.Members))
	for _, m := range d.Members {
		members = append(members, spaceMemberJSON{ID: m.ID, Name: m.Name})
	}
	return spaceDetailJSON{
		ID:       d.ID,
		Name:     d.Name,
		Personal: d.Personal,
		Members:  members,
		Counts:   spaceCountsJSON{Hosts: d.Counts.Hosts, RetiredHosts: d.Counts.RetiredHosts, Secrets: d.Counts.Secrets},
	}
}

var spaceMappings = []mapping{
	{api: reflect.TypeFor[api.SpaceAddResponse]()},
	{api: reflect.TypeFor[api.SpaceListResponse]()},
	{api: reflect.TypeFor[api.SpaceShowResponse]()},
	{api: reflect.TypeFor[api.SpaceRenameResponse]()},
	{api: reflect.TypeFor[api.SpaceRemoveResponse]()},
	{api: reflect.TypeFor[api.SpaceMemberAddResponse]()},
	{api: reflect.TypeFor[api.SpaceMemberRemoveResponse]()},
	{api: reflect.TypeFor[api.Space](), cli: reflect.TypeFor[spaceJSON]()},
	{api: reflect.TypeFor[api.SpaceDetail](), cli: reflect.TypeFor[spaceDetailJSON]()},
	{api: reflect.TypeFor[api.SpaceMember](), cli: reflect.TypeFor[spaceMemberJSON]()},
	{api: reflect.TypeFor[api.SpaceCounts](), cli: reflect.TypeFor[spaceCountsJSON]()},
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

// spaceList is what space ls prints: every Space, as a bare array. It leaves
// out the Members and counts space.list answers with each Space, since an
// older koko server answers without them and the CLI could not tell that from
// a Space with no Members; space show prints them.
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

// shownSpace is what space show prints: the Space with its Members and
// counts.
type shownSpace spaceDetailJSON

func (s shownSpace) Human(w io.Writer) error {
	kind := "shared"
	if s.Personal {
		kind = "Personal"
	}
	_, err := fmt.Fprintf(w, "Space %s (%s), %s\nMembers: %s\nHosts: %d\nRetired Hosts: %d\nSecrets: %d\n",
		s.Name, s.ID, kind, spaceDetailJSON(s).memberList(), s.Counts.Hosts, s.Counts.RetiredHosts, s.Counts.Secrets)
	return err
}

// memberList is the Members' names for people, or "none".
func (d spaceDetailJSON) memberList() string {
	if len(d.Members) == 0 {
		return "none"
	}
	names := make([]string, 0, len(d.Members))
	for _, m := range d.Members {
		names = append(names, m.Name)
	}
	return strings.Join(names, ", ")
}

func spaceShow(ctx context.Context, in *Input) (shownSpace, error) {
	resp, err := in.Call(ctx, api.SpaceShow, api.SpaceShowRequest{Name: in.Arg("space")})
	if err != nil {
		return shownSpace{}, err
	}
	return shownSpace(spaceDetailFromAPI(resp.Space)), nil
}

// renamedSpace is what space rename prints: the Space under its new name.
type renamedSpace struct {
	spaceJSON
	// from is the name it had, for people only.
	from string
}

func (s renamedSpace) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Renamed Space %s to %s (%s).\n", s.from, s.Name, s.ID)
	return err
}

func spaceRename(ctx context.Context, in *Input) (renamedSpace, error) {
	resp, err := in.Call(ctx, api.SpaceRename, api.SpaceRenameRequest{Name: in.Arg("space"), NewName: in.Arg("new-name")})
	if err != nil {
		return renamedSpace{}, err
	}
	return renamedSpace{spaceJSON: spaceFromAPI(resp.Space), from: in.Arg("space")}, nil
}

// removedSpace is what space rm prints: the Space it removed, as it was. An
// rm prints the record it removed in the shape its ls prints, so a script
// keeps the id of what is gone.
type removedSpace spaceJSON

func (s removedSpace) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Removed Space %s (%s).\n", s.Name, s.ID)
	return err
}

func spaceRm(ctx context.Context, in *Input) (removedSpace, error) {
	resp, err := in.Call(ctx, api.SpaceRemove, api.SpaceRemoveRequest{Name: in.Arg("space")})
	if err != nil {
		return removedSpace{}, err
	}
	return removedSpace(spaceFromAPI(resp.Space)), nil
}

// spaceMembers is what space member add and rm print: the Space with its
// Members after the change, in space show's shape.
type spaceMembers spaceDetailJSON

func (s spaceMembers) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Members of Space %s: %s.\n", s.Name, spaceDetailJSON(s).memberList())
	return err
}

func spaceMemberAdd(ctx context.Context, in *Input) (spaceMembers, error) {
	resp, err := in.Call(ctx, api.SpaceMemberAdd, api.SpaceMemberAddRequest{Space: in.Arg("space"), Users: in.Args("user")})
	if err != nil {
		return spaceMembers{}, err
	}
	return spaceMembers(spaceDetailFromAPI(resp.Space)), nil
}

func spaceMemberRm(ctx context.Context, in *Input) (spaceMembers, error) {
	resp, err := in.Call(ctx, api.SpaceMemberRemove, api.SpaceMemberRemoveRequest{Space: in.Arg("space"), Users: in.Args("user")})
	if err != nil {
		return spaceMembers{}, err
	}
	return spaceMembers(spaceDetailFromAPI(resp.Space)), nil
}

func spaceLs(ctx context.Context, in *Input) (spaceList, error) {
	resp, err := in.Call(ctx, api.SpaceList, api.SpaceListRequest{})
	if err != nil {
		return nil, err
	}
	l := make(spaceList, 0, len(resp.Spaces))
	for _, s := range resp.Spaces {
		l = append(l, spaceFromAPI(api.Space{ID: s.ID, Name: s.Name, Personal: s.Personal}))
	}
	return l, nil
}
