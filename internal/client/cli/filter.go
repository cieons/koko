package cli

import (
	"slices"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// filterTerm is one form of a filter term: how it is written, the API
// operator it reads as, and what it matches.
type filterTerm struct {
	form    string
	op      string
	matches string
}

// filterTerms are the forms of a filter term, for koko skill and for
// messages; readTerm reads each into its operator.
var filterTerms = []filterTerm{
	{"key=value", api.FilterEq, "the key's value equals value"},
	{"key!=value", api.FilterNe, "the key's value differs from value, or the record lacks the key"},
	{"key.lt=value", api.FilterLt, "the key's value is less than value"},
	{"key.le=value", api.FilterLe, "the key's value is at most value"},
	{"key.gt=value", api.FilterGt, "the key's value is greater than value"},
	{"key.ge=value", api.FilterGe, "the key's value is at least value"},
	{"key.has", api.FilterHas, "the record has the key"},
	{"key.lacks", api.FilterLacks, "the record lacks the key"},
}

// termForms names the filter terms, for messages.
var termForms = func() string {
	forms := make([]string, len(filterTerms))
	for i, f := range filterTerms {
		forms[i] = f.form
	}
	return strings.Join(forms[:len(forms)-1], ", ") + " or " + forms[len(forms)-1]
}()

// readFilters reads the filter terms of a list command into the API's,
// checking each against the fields the list filters on (Command.Filters)
// with their types, and the Attribute key rules. It refuses before any call
// a malformed term, a key the list does not take and a value not of its
// key's type, and resolves a relative date in the caller's time zone, since
// the API compares only absolute dates. No term needs quoting in a shell.
func readFilters(in *Input) ([]api.Filter, error) {
	var filters []api.Filter
	for _, word := range in.Terms() {
		f, err := readTerm(in, in.line.cmd.Filters, word)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, nil
}

// readTerm reads one filter term.
func readTerm(in *Input, fields map[string]record.Type, word string) (api.Filter, error) {
	left, value, hasValue := strings.Cut(word, "=")
	key, op := left, api.FilterEq
	dotted := false
	if k, ok := strings.CutSuffix(left, "!"); ok && hasValue {
		key, op = k, api.FilterNe
	} else if k, o, ok := strings.CutLast(left, "."); ok {
		key, op, dotted = k, o, true
	}
	switch {
	case dotted && (op == api.FilterHas || op == api.FilterLacks):
		if hasValue {
			return api.Filter{}, usagef("the term %s: %s.%s takes no value; write %s.%s", word, key, op, key, op)
		}
	case dotted && (op == api.FilterLt || op == api.FilterLe || op == api.FilterGt || op == api.FilterGe):
		if !hasValue {
			return api.Filter{}, usagef("the term %s needs a value: write %s=<value>", word, word)
		}
	case dotted:
		return api.Filter{}, usagef("the term %s: .%s is no operator; a term is %s", word, op, termForms)
	}
	typ, err := keyType(in, fields, key, "the term "+word)
	if err != nil {
		return api.Filter{}, err
	}
	f := api.Filter{Key: key, Op: op}
	if op == api.FilterHas || op == api.FilterLacks {
		return f, nil
	}
	if value == "" {
		return api.Filter{}, usagef("the value of the term %s cannot be empty; to find what lacks %s, write %s.lacks", word, key, key)
	}
	v, err := in.parseValue(typ, value)
	if err != nil {
		return api.Filter{}, usagef("the value of the term %s: %v", word, err)
	}
	f.Value = &v
	return f, nil
}

// readSort reads the values of --sort, key or key.desc, into the API's sort
// keys, refusing before any call a key the list does not take, and a key
// sorted by twice.
func readSort(in *Input) ([]api.SortKey, error) {
	fields := in.line.cmd.Filters
	var keys []api.SortKey
	for _, s := range in.Sorts() {
		key, desc := strings.CutSuffix(s, ".desc")
		written := "--sort " + s
		if strings.Contains(key, ".") {
			return nil, usagef("%s: write --sort <key>, or --sort <key>.desc from the greatest value down", written)
		}
		if _, err := keyType(in, fields, key, written); err != nil {
			return nil, err
		}
		if slices.ContainsFunc(keys, func(k api.SortKey) bool { return k.Key == key }) {
			return nil, usagef("%s: the list is sorted by %s already", written, key)
		}
		keys = append(keys, api.SortKey{Key: key, Desc: desc})
	}
	return keys, nil
}

// keyType returns the type of the values of key, which written filters or
// sorts on: a field's of fields, or an Attribute key's.
func keyType(in *Input, fields map[string]record.Type, key, written string) (record.Type, error) {
	t, err := record.KeyType(fields, key, "koko "+in.line.cmd.Path())
	if err != nil {
		return record.Type{}, usagef("%s: %v", written, err)
	}
	return t, nil
}
