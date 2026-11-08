package cli

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// The properties koko skill, koko help and koko completion keep for every
// entry of the command tree, so that a command added to the tree reaches
// them without touching these tests (rule 11 in docs/subsystems/code-layout.md).

// runOffline runs one koko command line on a machine with no koko server
// and no join, under a temporary HOME, so that any call would fail.
func runOffline(t *testing.T, args ...string) (stdout, stderr string, status int) {
	t.Helper()
	r := Runner{SocketPath: filepath.Join(t.TempDir(), "none.sock")}
	var out, errs bytes.Buffer
	status = r.Run(t.Context(), Invocation{
		Args:   args,
		Stdin:  strings.NewReader(""),
		Stdout: &out,
		Stderr: &errs,
		Env:    []string{"HOME=" + t.TempDir()},
	})
	return out.String(), errs.String(), status
}

// skillReference runs koko skill --json and decodes what it prints.
func skillReference(t *testing.T) skillJSON {
	t.Helper()
	out, errs, status := runOffline(t, "skill", "--json")
	if status != exitOK || errs != "" {
		t.Fatalf("koko skill --json: exit status %d, stderr %q", status, errs)
	}
	var s skillJSON
	if err := json.Unmarshal([]byte(out), &s, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("koko skill --json = %s: %v", out, err)
	}
	return s
}

// skillSections cuts koko skill's text into its sections, by heading.
func skillSections(t *testing.T) map[string]string {
	t.Helper()
	out, errs, status := runOffline(t, "skill")
	if status != exitOK || errs != "" {
		t.Fatalf("koko skill: exit status %d, stderr %q", status, errs)
	}
	sections := map[string]string{}
	heading := ""
	for line := range strings.Lines(out) {
		if h, ok := strings.CutPrefix(line, "#"); ok {
			heading = strings.TrimSpace(strings.TrimLeft(h, "#"))
			continue
		}
		sections[heading] += line
	}
	return sections
}

// listedCommands returns the paths of the commands a section of koko skill
// lists, one per item: "- `koko space add <space>`: ...", whose path is the
// words before the first argument or flag.
func listedCommands(section string) []string {
	var paths []string
	for line := range strings.Lines(section) {
		usage, ok := strings.CutPrefix(line, "- `koko ")
		if !ok {
			continue
		}
		usage, _, _ = strings.Cut(usage, "`")
		var path []string
		for w := range strings.FieldsSeq(usage) {
			if strings.ContainsAny(w[:1], "<[-") {
				break
			}
			path = append(path, w)
		}
		paths = append(paths, strings.Join(path, " "))
	}
	return paths
}

// Every command of the tree is listed under its own risk tier, and under no
// other, in koko skill's text and in its --json, so that a person can allow
// the read tier by prefix.
func TestSkillListsEveryCommandUnderItsTier(t *testing.T) {
	t.Parallel()
	sections := skillSections(t)
	ref := skillReference(t)
	tiers := []Tier{Read, Reversible, Irreversible}
	for _, c := range Tree() {
		for _, tier := range tiers {
			listed := slices.Contains(listedCommands(sections[tier.String()]), c.Path())
			if listed != (tier == c.Tier) {
				t.Errorf("koko skill lists koko %s under %s: %v; its tier is %s", c.Path(), tier, listed, c.Tier)
			}
		}
		i := slices.IndexFunc(ref.Commands, func(j commandJSON) bool { return j.Command == c.Path() })
		if i < 0 {
			t.Errorf("koko skill --json leaves out koko %s", c.Path())
			continue
		}
		if got := ref.Commands[i]; got.Tier != c.Tier.String() || got.OwnerOnly != c.OwnerOnly || got.Summary != c.Summary {
			t.Errorf("koko skill --json lists koko %s as %+v, want tier %s, owner only %v and its summary", c.Path(), got, c.Tier, c.OwnerOnly)
		}
	}
	if len(ref.Commands) != len(Tree()) {
		t.Errorf("koko skill --json lists %d commands, want the tree's %d", len(ref.Commands), len(Tree()))
	}
}

// Every command shows its usage, each of its flags and the shape of its
// --json, which holds what encoding/json/v2 writes for it, so that an agent
// learns the CLI's shape (ADR 0016) from the binary.
func TestSkillShowsEveryCommandsFlagsAndJSONShape(t *testing.T) {
	t.Parallel()
	sections := skillSections(t)
	text := sections[Read.String()] + sections[Reversible.String()] + sections[Irreversible.String()]
	ref := skillReference(t)
	for _, c := range Tree() {
		i := slices.IndexFunc(ref.Commands, func(j commandJSON) bool { return j.Command == c.Path() })
		if i < 0 {
			continue // TestSkillListsEveryCommandUnderItsTier says so
		}
		got := ref.Commands[i]
		if !strings.Contains(text, "- `"+got.Usage+"`") {
			t.Errorf("koko skill does not show koko %s as %s", c.Path(), got.Usage)
		}
		for _, f := range c.flags() {
			if !strings.Contains(got.Usage, "--"+f.Name) || !strings.Contains(text, "  - `"+f.usage()+"`: "+f.Summary) {
				t.Errorf("koko skill does not show koko %s's flag --%s", c.Path(), f.Name)
			}
		}
		for _, a := range c.Args {
			if !strings.Contains(got.Usage, "<"+a.Name+">") {
				t.Errorf("koko skill's usage of koko %s, %s, leaves out <%s>", c.Path(), got.Usage, a.Name)
			}
		}

		typ := c.Run.Shape()
		if (typ == nil) != (got.JSON == nil) {
			t.Errorf("koko skill --json gives koko %s the shape %v; the command prints %v", c.Path(), got.JSON, typ)
			continue
		}
		if typ == nil {
			continue
		}
		s := *got.JSON
		if anyType.MatchString(s) {
			t.Errorf("koko %s prints %s, whose shape %s holds a type shape cannot write", c.Path(), typ, s)
		}
		// The members a zero value of the type marshals to are the shape's
		// members that may not be left out. A type that marshals itself
		// says its shape instead (shaped), and has no any above.
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Struct {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || marshalsItself(typ) {
			continue
		}
		b, err := json.Marshal(reflect.New(typ).Elem().Interface())
		if err != nil {
			t.Fatalf("marshal a zero %s: %v", typ, err)
		}
		var members map[string]jsontext.Value
		if err := json.Unmarshal(b, &members); err != nil {
			t.Fatalf("%s marshals to %s: %v", typ, b, err)
		}
		for _, f := range jsonFields(typ) {
			_, marshalled := members[f.name]
			if marshalled == f.optional {
				t.Errorf("koko %s's shape has %q optional: %v, but a zero %s marshals to %s", c.Path(), f.name, f.optional, typ, b)
			}
			delete(members, f.name)
			if !strings.Contains(s, `"`+f.name+`"`) {
				t.Errorf("koko %s's shape %s leaves out %q", c.Path(), s, f.name)
			}
		}
		for name := range members {
			t.Errorf("a zero %s marshals the member %q, which koko %s's shape %s leaves out", typ, name, c.Path(), s)
		}
	}
}

// anyType matches the type any in a shape, which is no member's quoted name.
var anyType = regexp.MustCompile(`(^|[^"\w])any\b`)

// The shapes are written as the notation says.
func TestShapeNotation(t *testing.T) {
	t.Parallel()
	type inner struct {
		A string `json:"a"`
	}
	type sample struct {
		inner
		Count   int                     `json:"count"`
		On      bool                    `json:"on"`
		Maybe   *string                 `json:"maybe"`
		Twice   **string                `json:"twice,omitzero"`
		List    []string                `json:"list"`
		Bytes   []byte                  `json:"bytes"`
		Attrs   map[string]record.Value `json:"attrs"`
		Nested  *inner                  `json:"nested"`
		Skipped string                  `json:"-"`
		hidden  string
	}
	_ = sample{}.hidden
	want := `{"a": string, "count": number, "on": boolean, "maybe": string | null, "twice"?: string | null, ` +
		`"list": [string], "bytes": string, "attrs": {<key>: string | number}, "nested": {"a": string} | null}`
	if got := shape(reflect.TypeFor[sample]()); got != want {
		t.Errorf("shape = %s\nwant    %s", got, want)
	}

	// A type that marshals itself is written by its jsonShape, or any
	// without one, which TestSkillShowsEveryCommandsFlagsAndJSONShape
	// refuses.
	if got := shape(reflect.TypeFor[selfShaped]()); got != "string | [string]" {
		t.Errorf("shape of a shaped type = %s, want its jsonShape", got)
	}
	if got := shape(reflect.TypeFor[selfMarshalled]()); got != "any" {
		t.Errorf("shape of a type that marshals itself, unshaped = %s, want any", got)
	}
}

type selfMarshalled struct{ one bool }

func (s selfMarshalled) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.Bool(s.one))
}

type selfShaped struct{ selfMarshalled }

func (selfShaped) jsonShape() string { return "string | [string]" }

// Every error code, the koko server's and the CLI's own, and every exit
// status are listed, so that an agent knows what to branch on.
func TestSkillListsEveryErrorCodeAndExitStatus(t *testing.T) {
	t.Parallel()
	sections := skillSections(t)
	ref := skillReference(t)
	want := map[string]string{}
	for _, c := range api.Codes() {
		want[c.Name] = "server"
	}
	for _, c := range cliCodes {
		want[c] = "cli"
	}
	for code, from := range want {
		if !strings.Contains(sections["Error codes"], "`"+code+"`") {
			t.Errorf("koko skill does not list the error code %s", code)
		}
		if !slices.Contains(ref.ErrorCodes, errorCodeJSON{Code: code, From: from}) {
			t.Errorf("koko skill --json does not list the error code %s from the %s", code, from)
		}
	}
	if len(ref.ErrorCodes) != len(want) {
		t.Errorf("koko skill --json lists %d error codes, want %d", len(ref.ErrorCodes), len(want))
	}

	// The statuses are the CLI's contract (ADR 0019): 0, 1 and 2 at least.
	var statuses []int
	for _, e := range ref.ExitStatuses {
		statuses = append(statuses, e.Status)
		if e.Means == "" || !strings.Contains(sections["Exit statuses"], fmt.Sprintf("- `%d`: %s", e.Status, e.Means)) {
			t.Errorf("koko skill does not say what exit status %d means", e.Status)
		}
	}
	for _, s := range []int{0, 1, 2} {
		if !slices.Contains(statuses, s) {
			t.Errorf("koko skill --json lists the exit statuses %v, without %d", statuses, s)
		}
	}
}

// Every Preset Attribute is listed with its type, and the filter syntax
// with each form of a term, which the parser reads as the operator it says,
// and with the fields each list filters on.
func TestSkillDocumentsThePresetsAndTheFilterSyntax(t *testing.T) {
	t.Parallel()
	sections := skillSections(t)
	ref := skillReference(t)
	presets := record.Presets()
	if len(ref.Presets) != len(presets) {
		t.Errorf("koko skill --json lists %d Presets, want %d", len(ref.Presets), len(presets))
	}
	for i, p := range presets {
		if i < len(ref.Presets) && (ref.Presets[i].Key != p.Key || ref.Presets[i].Type != string(p.Type.Kind())) {
			t.Errorf("koko skill --json lists the Preset %+v, want %s of type %s", ref.Presets[i], p.Key, p.Type.Kind())
		}
		if !strings.Contains(sections["Preset Attributes"], "- `"+p.Key+"`: "+string(p.Type.Kind())) {
			t.Errorf("koko skill does not list the Preset %s with its type %s", p.Key, p.Type.Kind())
		}
	}

	ops := map[string]string{}
	for _, term := range ref.Filters.Terms {
		if !strings.Contains(sections["Filtering and sorting lists"], "- `"+term.Term+"`: "+term.Matches) {
			t.Errorf("koko skill does not list the term %s", term.Term)
		}
		word := strings.NewReplacer("key", "region", "value", "eu").Replace(term.Term)
		in := inputFor(t, filtersTree, "things", "ls", word)
		f, err := readTerm(in, in.line.cmd.Filters, word)
		if err != nil {
			t.Errorf("the term %s, written %s, does not parse: %v", term.Term, word, err)
			continue
		}
		ops[f.Op] = term.Term
	}
	for _, op := range []string{api.FilterEq, api.FilterNe, api.FilterLt, api.FilterLe, api.FilterGt, api.FilterGe, api.FilterHas, api.FilterLacks} {
		if _, ok := ops[op]; !ok {
			t.Errorf("koko skill lists no term that reads as the operator %s", op)
		}
	}

	for _, c := range Tree() {
		i := slices.IndexFunc(ref.Commands, func(j commandJSON) bool { return j.Command == c.Path() })
		if i < 0 || c.Filters == nil {
			continue
		}
		got := ref.Commands[i].Filters
		if got == nil || len(*got) != len(c.Filters) {
			t.Errorf("koko skill --json gives koko %s the filter fields %v, want %d", c.Path(), got, len(c.Filters))
			continue
		}
		for _, k := range *got {
			if typ, ok := c.Filters[k.Key]; !ok || k.Type != string(typ.Kind()) {
				t.Errorf("koko skill --json says koko %s filters on %+v", c.Path(), k)
			}
		}
	}
}

// koko skill, koko help and koko completion only print, and koko skill
// install writes one file koko owns (P0 spec: overview).
func TestSkillCommandsSitInTheirTiers(t *testing.T) {
	t.Parallel()
	want := map[string]Tier{"skill": Read, "skill install": Reversible, "help": Read, "completion": Read}
	for path, tier := range want {
		c := find(Tree(), strings.Fields(path))
		if c == nil {
			t.Errorf("koko %s is not in the tree", path)
			continue
		}
		if c.Tier != tier || c.Op != nil {
			t.Errorf("koko %s is %s and calls %v, want %s and no call", path, c.Tier, c.Op, tier)
		}
	}
}

// koko help lists every command, and koko help <command> shows its usage
// and each of its flags, all from the tree.
func TestHelpShowsEveryCommandAndItsFlags(t *testing.T) {
	t.Parallel()
	out, errs, status := runOffline(t, "help")
	if status != exitOK || errs != "" {
		t.Fatalf("koko help: exit status %d, stderr %q", status, errs)
	}
	for _, c := range Tree() {
		if !strings.Contains(out, "  "+c.Path()+" ") || !strings.Contains(out, c.Summary) {
			t.Errorf("koko help does not list koko %s with its summary", c.Path())
		}
		args := append([]string{"help"}, c.words()...)
		detail, errs, status := runOffline(t, args...)
		if status != exitOK || errs != "" {
			t.Errorf("koko help %s: exit status %d, stderr %q", c.Path(), status, errs)
			continue
		}
		if !strings.HasPrefix(detail, synopsis(&c)+"\n") || !strings.Contains(detail, "Risk tier: "+c.Tier.String()) {
			t.Errorf("koko help %s = %q, want its usage and tier", c.Path(), detail)
		}
		for _, f := range c.flags() {
			if !strings.Contains(detail, "  "+f.usage()) {
				t.Errorf("koko help %s does not show --%s", c.Path(), f.Name)
			}
		}
		for _, d := range Tree() {
			if strings.HasPrefix(d.Path(), c.Path()+" ") && !strings.Contains(detail, "run koko help "+d.Path()) {
				t.Errorf("koko help %s does not name the longer command koko %s", c.Path(), d.Path())
			}
		}
		js, _, _ := runOffline(t, append(args, "--json")...)
		var l []commandJSON
		if err := json.Unmarshal([]byte(js), &l, json.RejectUnknownMembers(true)); err != nil || len(l) != 1 || l[0].Command != c.Path() {
			t.Errorf("koko help %s --json = %s (%v), want that command alone", c.Path(), js, err)
		}
	}

	out, _, _ = runOffline(t, "help", "--json")
	var all []commandJSON
	if err := json.Unmarshal([]byte(out), &all, json.RejectUnknownMembers(true)); err != nil || len(all) != len(Tree()) {
		t.Errorf("koko help --json lists %d commands (%v), want the tree's %d", len(all), err, len(Tree()))
	}
}

// completeIn loads the script koko completion writes for shell into that
// shell, and returns what it offers in place of the last word of each
// query, the words after koko. It skips the test where the shell is not
// installed.
func completeIn(t *testing.T, shell string, queries [][]string) [][]string {
	t.Helper()
	sh, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s is not installed: %v", shell, err)
	}
	script, errs, status := runOffline(t, "completion", shell)
	if status != exitOK || errs != "" {
		t.Fatalf("koko completion %s: exit status %d, stderr %q", shell, status, errs)
	}
	path := filepath.Join(t.TempDir(), "koko-completion")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	var in strings.Builder
	for _, q := range queries {
		quoted := make([]string, len(q))
		for i, w := range q {
			quoted[i] = shellJoin([]string{w})
		}
		in.WriteString(strings.Join(quoted, " ") + "\n")
	}
	// compdef is zsh's completion system's, which a plain zsh lacks.
	driver := `compdef() { :; }
. "$1"
while IFS= read -r line; do
	eval "set -- $line"
	echo $(_koko_complete "$@")
done`
	args := []string{"--noprofile", "--norc", "-c", driver, "complete", path}
	if shell == "zsh" {
		args = []string{"-f", "-c", driver, "complete", path}
	}
	cmd := exec.CommandContext(t.Context(), sh, args...)
	cmd.Stdin = strings.NewReader(in.String())
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s with the completion script: %v", shell, err)
	}
	var got [][]string
	for line := range strings.Lines(string(out)) {
		got = append(got, strings.Fields(line))
	}
	if len(got) != len(queries) {
		t.Fatalf("%s answered %d queries of %d: %q", shell, len(got), len(queries), out)
	}
	return got
}

// The completion script of each shell offers every command's words, its
// flags, and the values of each flag and argument that has choices, all
// from the tree; an argument never extends the command path.
func TestCompletionCompletesEveryCommandFromTheTree(t *testing.T) {
	t.Parallel()
	type query struct {
		words []string
		// want are words the answer holds, or the answer whole when exact.
		want  []string
		exact bool
		// plain says the answer holds words, not a flag value's :file,
		// :dir or :value.
		plain bool
		// without are words the answer must not hold.
		without []string
	}
	var queries []query
	tree := Tree()
	for _, c := range tree {
		w := c.words()
		then := func(words ...string) []string { return append(slices.Clone(w), words...) }
		for j := range w {
			queries = append(queries, query{words: append(slices.Clone(w[:j]), ""), want: w[j : j+1]})
		}
		var flags []string
		if c.takesJSON() {
			flags = append(flags, "--json")
		}
		for _, f := range c.flags() {
			flags = append(flags, "--"+f.Name)
		}
		queries = append(queries, query{words: then("--"), want: flags, exact: true})
		for _, f := range c.flags() {
			value := then("--"+f.Name, "")
			switch {
			case f.Bool:
				queries = append(queries, query{words: value, plain: true})
				continue
			case len(f.Choices) > 0:
				queries = append(queries, query{words: value, want: f.Choices, exact: true})
			case f.Value == "file" || f.Value == "dir":
				queries = append(queries, query{words: value, want: []string{":" + f.Value}, exact: true})
			default:
				queries = append(queries, query{words: value, want: []string{":value"}, exact: true})
			}
			// The word after the value is no longer the flag's.
			queries = append(queries,
				query{words: then("--"+f.Name, "v", ""), plain: true},
				query{words: then("--"+f.Name, "v", "--"), want: flags, exact: true})
		}
		for j, a := range c.Args {
			if len(a.Choices) > 0 {
				words := slices.Clone(w)
				for range j {
					words = append(words, "x")
				}
				queries = append(queries, query{words: append(words, ""), want: a.Choices})
			}
		}
		// After an argument, the path's longer commands are not offered.
		var longer []string
		for _, d := range tree {
			if dw := d.words(); len(dw) > len(w) && slices.Equal(dw[:len(w)], w) {
				longer = append(longer, dw[len(w)])
			}
		}
		queries = append(queries, query{words: then("x", ""), without: longer})
	}
	for _, shell := range completionShells {
		t.Run(shell, func(t *testing.T) {
			var words [][]string
			for _, q := range queries {
				words = append(words, q.words)
			}
			got := completeIn(t, shell, words)
			for i, q := range queries {
				if q.plain && slices.ContainsFunc(got[i], func(w string) bool { return strings.HasPrefix(w, ":") }) {
					t.Errorf("koko %s<TAB> offers %q, a value, after a flag that takes none", strings.Join(q.words, " "), got[i])
				}
				if q.exact && !sameWords(got[i], q.want) {
					t.Errorf("koko %s<TAB> offers %q, want %q", strings.Join(q.words, " "), got[i], q.want)
				}
				for _, w := range q.want {
					if !slices.Contains(got[i], w) {
						t.Errorf("koko %s<TAB> offers %q, without %s", strings.Join(q.words, " "), got[i], w)
					}
				}
				for _, w := range q.without {
					if slices.Contains(got[i], w) {
						t.Errorf("koko %s<TAB> offers %q, with %s", strings.Join(q.words, " "), got[i], w)
					}
				}
			}
		})
	}
}

func sameWords(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// In bash, the script filters what it offers by the word typed, and
// completes a file flag's value with the files there; the lines it
// completes are taken from the tree.
func TestCompletionInBash(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is not installed: %v", err)
	}
	script, _, _ := runOffline(t, "completion", "bash")
	dir := t.TempDir()
	path := filepath.Join(dir, "koko-completion")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	type line struct {
		words []string
		want  string
	}
	var lines []line
	for _, c := range Tree() {
		w := c.words()
		// The last word of the path, typed but for its last letter.
		typed := append(slices.Clone(w[:len(w)-1]), w[len(w)-1][:len(w[len(w)-1])-1])
		lines = append(lines, line{append([]string{"koko"}, typed...), w[len(w)-1]})
		for _, f := range c.flags() {
			switch {
			case len(f.Choices) > 0:
				choice := f.Choices[len(f.Choices)-1]
				lines = append(lines, line{slices.Concat([]string{"koko"}, w, []string{"--" + f.Name, choice[:1]}), choice})
			case f.Value == "file":
				typed := filepath.Join(dir, "no")
				lines = append(lines, line{slices.Concat([]string{"koko"}, w, []string{"--" + f.Name, typed}), filepath.Join(dir, "note.txt")})
			}
		}
		for j, a := range c.Args {
			if len(a.Choices) > 0 && j == 0 {
				choice := a.Choices[len(a.Choices)-1]
				lines = append(lines, line{slices.Concat([]string{"koko"}, w, []string{choice[:1]}), choice})
			}
		}
	}
	for _, l := range lines {
		quoted := make([]string, len(l.words))
		for i, w := range l.words {
			quoted[i] = shellJoin([]string{w})
		}
		driver := `. "$1"; COMP_WORDS=(` + strings.Join(quoted, " ") + `); COMP_CWORD=$((${#COMP_WORDS[@]} - 1)); _koko; printf '%s\n' "${COMPREPLY[@]}"`
		cmd := exec.CommandContext(t.Context(), sh, "--noprofile", "--norc", "-c", driver, "complete", path)
		cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
		out, err := cmd.Output()
		if err != nil || !slices.Contains(strings.Split(strings.TrimSpace(string(out)), "\n"), l.want) {
			t.Errorf("%s<TAB> in bash offers %q (%v), without %q", strings.Join(l.words, " "), out, err, l.want)
		}
	}
}

// filtersTree is a tree with one list that takes filter terms.
var filtersTree = []Command{{Noun: "things", Verb: "ls", Tier: Read, Filters: map[string]record.Type{"name": record.Text}}}
