package cli

import (
	"context"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/shared/record"
)

// hostLsAction runs host ls: its --json is the list, and --ansible, which
// takes no --json, prints the inventory in its place.
var hostLsAction = Action{
	run: func(ctx context.Context, in *Input) (Result, error) {
		if in.Bool("ansible") {
			return hostAnsible(ctx, in)
		}
		return hostLs(ctx, in)
	},
	shape: action(hostLs).shape,
}

// ansibleInventory is what host ls --ansible prints: Ansible's dynamic
// inventory in the form a script's --list gives, with _meta.hostvars, so
// that Ansible never calls it with --host (ADR 0059).
type ansibleInventory struct {
	// hostvars holds each Host's variables, by its name.
	hostvars map[string]map[string]any
	// groups holds the names of each group's Hosts, in host ls's order.
	groups map[string][]string
}

func (inv ansibleInventory) Human(w io.Writer) error {
	out := map[string]any{
		"_meta": map[string]any{"hostvars": inv.hostvars},
		// Every group is a child of all, which Ansible would make it
		// anyway; all is printed even with no group.
		"all": map[string]any{"children": slices.Sorted(maps.Keys(inv.groups))},
	}
	for g, hosts := range inv.groups {
		out[g] = map[string]any{"hosts": hosts}
	}
	return encodeJSON(w, out)
}

func hostAnsible(ctx context.Context, in *Input) (ansibleInventory, error) {
	for _, flag := range []string{"all", "with-note"} {
		if in.Bool(flag) {
			return ansibleInventory{}, usagef("--ansible takes no --%s: it lists the Managed Hosts without their Notes", flag)
		}
	}
	if in.line.json {
		return ansibleInventory{}, usagef("--ansible takes no --json: the inventory is JSON already")
	}
	l, err := hostLs(ctx, in)
	if err != nil {
		return ansibleInventory{}, err
	}
	inv := ansibleInventory{hostvars: map[string]map[string]any{}, groups: map[string][]string{}}
	join := func(group, host string) {
		if !slices.Contains(inv.groups[group], host) {
			inv.groups[group] = append(inv.groups[group], host)
		}
	}
	for _, h := range l {
		// koko reaches only a Managed Host.
		if h.State != "managed" {
			continue
		}
		// No ansible_user, port or password: the Gateway takes only the
		// user name koko, and logs in as the Host Account.
		vars := map[string]any{"ansible_host": h.Name + ".koko", "koko_space": h.Space}
		join(ansibleName("space_"+h.Space), h.Name)
		for k, v := range h.Attributes {
			// An Attribute key is never a field's name, space included, so
			// koko_<key> never stands for koko_space.
			vars[ansibleName("koko_"+k)] = v
			if kind := record.TypeOf(k).Kind(); kind != record.KindDate && kind != record.KindNumber {
				join(ansibleName(k+"_"+v.String()), h.Name)
			}
		}
		inv.hostvars[h.Name] = vars
	}
	return inv, nil
}

// ansibleName gives s as Ansible takes a variable or a group name: each
// character other than an ASCII letter, a digit or '_' becomes '_'. s
// always starts with a letter, from an Attribute key or a fixed prefix.
func ansibleName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, s)
}
