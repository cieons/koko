package api_test

// The generator of the web UI's TypeScript types. Each Go file of package api
// becomes one TypeScript file in web/src/api, holding what the API carries of
// the declarations in it: its request, response and record types as the
// API's JSON (encoding/json/v2) shapes them, its operations, and its
// registered error codes.

import (
	"encoding"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// generateTypeScript returns the TypeScript files generated from the Go files
// of package api in dir, by file name.
func generateTypeScript(dir string) (map[string]string, error) {
	src, err := parseSource(dir)
	if err != nil {
		return nil, err
	}
	ops := map[string]api.OpInfo{}
	for _, o := range api.Operations() {
		ops[o.Info().Name] = o.Info()
	}
	codes := map[string]bool{}
	for _, c := range api.Codes() {
		codes[c.Name] = true
	}
	// endpoints are the web front's own JSON endpoints, under /auth/ and
	// /terminal/files/, by their request type's name, which is each one's own.
	endpoints := map[string]api.AuthEndpointInfo{}
	for _, e := range slices.Concat(api.AuthEndpoints(), api.FilesEndpoints()) {
		endpoints[e.Request.Name()] = e
	}
	structs := map[string]reflect.Type{}
	for _, o := range ops {
		collectStructs(o.Request, structs)
		collectStructs(o.Response, structs)
	}
	for _, e := range endpoints {
		collectStructs(e.Request, structs)
		collectStructs(e.Response, structs)
	}
	collectStructs(reflect.TypeFor[api.ErrorResponse](), structs)

	out := map[string]string{}
	found := map[string]bool{}
	foundCodes := map[string]bool{}
	foundEndpoints := map[string]bool{}
	for _, f := range src.files {
		w := &tsWriter{module: f.module, src: src, imports: map[string]map[string]bool{}}
		for _, d := range f.decls {
			var err error
			switch d.kind {
			case declOp:
				if o, ok := ops[d.opName]; ok {
					found[d.opName] = true
					err = w.op(d, o)
				}
			case declEndpoint:
				if e, ok := endpoints[d.request]; ok {
					foundEndpoints[d.request] = true
					err = w.endpoint(d, e)
				}
			case declCode:
				if codes[d.code] {
					foundCodes[d.code] = true
					w.code(d)
				}
			case declType:
				if d.name == "Op" {
					w.opInterface(d)
				} else if d.name == "AuthEndpoint" {
					w.endpointInterface(d)
				} else if t, ok := structs[d.name]; ok {
					err = w.typeDecl(d, t)
				}
			}
			if err != nil {
				return nil, fmt.Errorf("%s.go: %s: %w", f.module, d.name, err)
			}
		}
		if w.body.Len() > 0 {
			out[f.module+".ts"] = w.String()
		}
	}
	for _, registered := range []struct {
		kind, literal string
		names         []string
		found         map[string]bool
	}{
		{"operation", "an Op", slices.Sorted(maps.Keys(ops)), found},
		{"error code", "a Code", slices.Sorted(maps.Keys(codes)), foundCodes},
		{"web front endpoint of request", "an AuthEndpoint", slices.Sorted(maps.Keys(endpoints)), foundEndpoints},
	} {
		for _, name := range registered.names {
			if !registered.found[name] {
				return nil, fmt.Errorf("%s %s: no var declares it as %s literal in internal/shared/api", registered.kind, name, registered.literal)
			}
		}
	}
	return out, nil
}

// collectStructs records every named struct type of package api that values
// of t can hold, by name. tsType refuses the struct types of other packages.
func collectStructs(t reflect.Type, structs map[string]reflect.Type) {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		collectStructs(t.Elem(), structs)
	case reflect.Struct:
		if t.Name() != "" {
			if _, ok := structs[t.Name()]; ok || t.PkgPath() != apiPackage {
				return
			}
			structs[t.Name()] = t
		}
		for f := range t.Fields() {
			collectStructs(f.Type, structs)
		}
	}
}

// The source: what the generator reads from the Go files.

type declKind int

const (
	declType declKind = iota
	declOp
	declCode
	declEndpoint
)

// goDecl is one declaration of package api the generator may write.
type goDecl struct {
	kind      declKind
	name      string            // the Go name: Space, SpaceAdd, CodeUnknownSpace
	opName    string            // an operation's name, such as space.add
	code      string            // an error code's name, such as unknown_space
	request   string            // an /auth/ endpoint's request type, such as AuthSignInRequest
	doc       string            // the doc comment's text
	fieldDocs map[string]string // a struct's field docs, by Go field name
}

// goFile is one Go file's declarations, in source order.
type goFile struct {
	module string // the file's name without .go, and the TypeScript module's
	decls  []goDecl
}

type source struct {
	files      []goFile          // by file name
	typeModule map[string]string // each declared type's module, by type name
}

func parseSource(dir string) (*source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read package api: %w", err)
	}
	src := &source{typeModule: map[string]string{}}
	fset := token.NewFileSet()
	for _, e := range entries {
		module, ok := strings.CutSuffix(e.Name(), ".go")
		if !ok || strings.HasSuffix(module, "_test") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		gf := goFile{module: module}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					d := goDecl{kind: declType, name: s.Name.Name, doc: specDoc(gd, s.Doc)}
					if st, ok := s.Type.(*ast.StructType); ok {
						d.fieldDocs = fieldDocs(st)
					}
					gf.decls = append(gf.decls, d)
					src.typeModule[s.Name.Name] = module
				case *ast.ValueSpec:
					for i, n := range s.Names {
						if gd.Tok != token.VAR || i >= len(s.Values) {
							continue
						}
						if op := opLiteral(s.Values[i]); op != "" {
							gf.decls = append(gf.decls, goDecl{kind: declOp, name: n.Name, opName: op, doc: specDoc(gd, s.Doc)})
						} else if req := endpointLiteral(s.Values[i]); req != "" {
							gf.decls = append(gf.decls, goDecl{kind: declEndpoint, name: n.Name, request: req, doc: specDoc(gd, s.Doc)})
						} else if code := codeLiteral(s.Values[i]); code != "" {
							gf.decls = append(gf.decls, goDecl{kind: declCode, name: n.Name, code: code, doc: specDoc(gd, s.Doc)})
						}
					}
				}
			}
		}
		src.files = append(src.files, gf)
	}
	return src, nil
}

// specDoc is a type's or value's doc comment, which sits on the declaration
// when it is not in a group.
func specDoc(gd *ast.GenDecl, doc *ast.CommentGroup) string {
	if doc == nil && !gd.Lparen.IsValid() {
		doc = gd.Doc
	}
	return doc.Text()
}

func fieldDocs(st *ast.StructType) map[string]string {
	docs := map[string]string{}
	for _, f := range st.Fields.List {
		doc := f.Doc
		if doc == nil {
			doc = f.Comment
		}
		for _, n := range f.Names {
			docs[n.Name] = doc.Text()
		}
	}
	return docs
}

// opLiteral returns the Name of an Op[Req, Resp]{Name: ...} literal, or ""
// when e is not one.
func opLiteral(e ast.Expr) string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	typ, ok := lit.Type.(*ast.IndexListExpr)
	if !ok {
		return ""
	}
	if id, ok := typ.X.(*ast.Ident); !ok || id.Name != "Op" {
		return ""
	}
	return literalName(lit)
}

// endpointLiteral returns the request type's name of an
// AuthEndpoint[Req, Resp]{...} literal, or "" when e is not one.
func endpointLiteral(e ast.Expr) string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	typ, ok := lit.Type.(*ast.IndexListExpr)
	if !ok || len(typ.Indices) != 2 {
		return ""
	}
	if id, ok := typ.X.(*ast.Ident); !ok || id.Name != "AuthEndpoint" {
		return ""
	}
	req, ok := typ.Indices[0].(*ast.Ident)
	if !ok {
		return ""
	}
	return req.Name
}

// codeLiteral returns the Name of a Code{...} literal, such as
// Code{"unknown_space", http.StatusNotFound}, or "" when e is not one.
func codeLiteral(e ast.Expr) string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "Code" {
		return ""
	}
	return literalName(lit)
}

// literalName returns the string a struct literal gives its Name field, the
// first of both Op and Code: by key, or as its first element when the
// literal names no fields. It is "" when the literal gives no string.
func literalName(lit *ast.CompositeLit) string {
	var name ast.Expr
	for i, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Name" {
				name = kv.Value
			}
		} else if i == 0 {
			name = el
		}
	}
	val, ok := name.(*ast.BasicLit)
	if !ok || val.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(val.Value)
	if err != nil {
		return ""
	}
	return s
}

// The mapping: Go types to TypeScript types, as encoding/json/v2 shapes them.

var (
	timeType = reflect.TypeFor[time.Time]()
	// valueType is an Attribute value, which writes its own JSON: a JSON
	// number for a number, an exact decimal that a JavaScript number holds
	// exactly (record.MaxDecimalDigits), and a JSON string otherwise.
	valueType = reflect.TypeFor[record.Value]()
	// customMarshalers are the interfaces of a type that writes its own JSON.
	customMarshalers = []reflect.Type{
		reflect.TypeFor[json.Marshaler](),
		reflect.TypeFor[json.MarshalerTo](),
		reflect.TypeFor[encoding.TextMarshaler](),
	}
)

// tsType is the TypeScript type of t's JSON values; ref names a named struct
// type.
func tsType(t reflect.Type, ref func(reflect.Type) (string, error)) (string, error) {
	if t == timeType {
		return "string", nil // RFC 3339
	}
	if t == valueType {
		return "string | number", nil
	}
	if t.PkgPath() != "" && !strings.HasPrefix(t.PkgPath(), kokoModule) {
		return "", fmt.Errorf("%s is declared outside koko, with JSON rules of its own (time.Duration has none)", t)
	}
	for _, m := range customMarshalers {
		if t.Implements(m) || reflect.PointerTo(t).Implements(m) {
			return "", fmt.Errorf("%s has its own JSON or text form, which the generator cannot see", t)
		}
	}
	switch t.Kind() {
	case reflect.String:
		return "string", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number", nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string", nil // standard base64 in json/v2, such as a Secret value
		}
		elem, err := tsType(t.Elem(), ref)
		if err != nil {
			return "", err
		}
		if strings.Contains(elem, " ") {
			elem = "(" + elem + ")"
		}
		return elem + "[]", nil // a nil slice is [] in json/v2
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			break
		}
		elem, err := tsType(t.Elem(), ref)
		if err != nil {
			return "", err
		}
		return "Record<string, " + elem + ">", nil // a nil map is {} in json/v2
	case reflect.Pointer:
		elem, err := tsType(t.Elem(), ref)
		if err != nil {
			return "", err
		}
		return elem + " | null", nil
	case reflect.Struct:
		if t.Name() == "" {
			break // declare it as a named type instead
		}
		return ref(t)
	}
	return "", fmt.Errorf("the generator does not map %s yet: extend tsType in internal/shared/api/tsgen_test.go", t)
}

// tsField is one member of an object type.
type tsField struct {
	goName   string
	name     string
	typ      string
	optional bool
}

// tsFields lists the members of the JSON object of struct type t.
func tsFields(t reflect.Type, ref func(reflect.Type) (string, error)) ([]tsField, error) {
	var fields []tsField
	for f := range t.Fields() {
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		tag, hasTag := f.Tag.Lookup("json")
		if tag == "-" {
			continue
		}
		if f.Anonymous {
			return nil, fmt.Errorf("field %s: the generator does not write embedded fields yet", f.Name)
		}
		name, opts, _ := strings.Cut(tag, ",")
		if !hasTag || name == "" {
			name = f.Name
		}
		if !tsIdentifier(name) {
			return nil, fmt.Errorf("field %s: the generator does not write the JSON name %q yet", f.Name, name)
		}
		optional := false
		for opt := range strings.SplitSeq(opts, ",") {
			switch opt {
			case "":
			case "omitzero", "omitempty":
				optional = true
			default:
				return nil, fmt.Errorf("field %s: the generator does not map the JSON option %q yet", f.Name, opt)
			}
		}
		ft := f.Type
		if optional && ft.Kind() == reflect.Pointer {
			ft = ft.Elem() // a nil pointer is left out, never null
		}
		typ, err := tsType(ft, ref)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", f.Name, err)
		}
		fields = append(fields, tsField{goName: f.Name, name: name, typ: typ, optional: optional})
	}
	return fields, nil
}

// The writer: one TypeScript module.

type tsWriter struct {
	module  string
	src     *source
	imports map[string]map[string]bool // names, by module
	body    strings.Builder
}

// ref names a type of package api, importing it from its own module.
func (w *tsWriter) ref(t reflect.Type) (string, error) {
	module, ok := w.src.typeModule[t.Name()]
	if t.PkgPath() != apiPackage || !ok {
		return "", fmt.Errorf("%s is not declared in internal/shared/api", t)
	}
	w.use(module, t.Name())
	return t.Name(), nil
}

// use imports name from module, unless it is this module.
func (w *tsWriter) use(module, name string) {
	if module == w.module {
		return
	}
	if w.imports[module] == nil {
		w.imports[module] = map[string]bool{}
	}
	w.imports[module][name] = true
}

// tsIdentifier reports whether name can be written as a property name
// without quotes: ASCII letters, digits and '_', not starting with a digit.
func tsIdentifier(name string) bool {
	for i, r := range name {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_'
		digit := r >= '0' && r <= '9'
		if !letter && (!digit || i == 0) {
			return false
		}
	}
	return name != ""
}

var apiPackage = reflect.TypeFor[api.Space]().PkgPath()

const kokoModule = "github.com/cieons/koko/"

func (w *tsWriter) typeDecl(d goDecl, t reflect.Type) error {
	fields, err := tsFields(t, w.ref)
	if err != nil {
		return err
	}
	w.blank()
	jsdoc(&w.body, "", d.doc)
	if len(fields) == 0 {
		fmt.Fprintf(&w.body, "export type %s = Record<string, never>;\n", d.name)
		return nil
	}
	fmt.Fprintf(&w.body, "export interface %s {\n", d.name)
	for _, f := range fields {
		jsdoc(&w.body, "  ", d.fieldDocs[f.goName])
		name := f.name
		if f.optional {
			name += "?"
		}
		fmt.Fprintf(&w.body, "  %s: %s;\n", name, f.typ)
	}
	w.body.WriteString("}\n")
	return nil
}

func (w *tsWriter) op(d goDecl, o api.OpInfo) error {
	req, err := w.ref(o.Request)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	resp, err := w.ref(o.Response)
	if err != nil {
		return fmt.Errorf("response: %w", err)
	}
	w.use(w.src.typeModule["Op"], "Op")
	w.blank()
	jsdoc(&w.body, "", d.doc)
	fmt.Fprintf(&w.body, "export const %s: Op<%s, %s> = {\n  name: %q,\n  path: %q,\n  write: %t,\n};\n",
		d.name, req, resp, o.Name, api.PathPrefix+o.Name, o.Write)
	return nil
}

// endpoint writes a web front endpoint as an AuthEndpoint value, which the
// web UI posts to as it calls an Op.
func (w *tsWriter) endpoint(d goDecl, e api.AuthEndpointInfo) error {
	req, err := w.ref(e.Request)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	resp, err := w.ref(e.Response)
	if err != nil {
		return fmt.Errorf("response: %w", err)
	}
	w.use(w.src.typeModule["AuthEndpoint"], "AuthEndpoint")
	w.blank()
	jsdoc(&w.body, "", d.doc)
	fmt.Fprintf(&w.body, "export const %s: AuthEndpoint<%s, %s> = {\n  path: %q,\n};\n", d.name, req, resp, e.Path)
	return nil
}

// code writes an error code as a string constant under its Go name, which
// the web UI compares an ApiError's code with.
func (w *tsWriter) code(d goDecl) {
	w.blank()
	jsdoc(&w.body, "", d.doc)
	fmt.Fprintf(&w.body, "export const %s = %q;\n", d.name, d.code)
}

// opInterface writes the TypeScript twin of Op, which a client calls with.
func (w *tsWriter) opInterface(d goDecl) {
	w.blank()
	jsdoc(&w.body, "", d.doc)
	w.body.WriteString(`export interface Op<Req, Resp> {
  /** The operation's name, "<noun>.<verb>", such as "space.add". */
  readonly name: string;
  /** The operation is a POST to this path, such as /v1/space.add. */
  readonly path: string;
  /** A write may change the record; a read never does. */
  readonly write: boolean;
  /** Never set: it carries the request and response types to a client. */
  readonly types?: { request: Req; response: Resp };
}
`)
}

// endpointInterface writes the TypeScript twin of AuthEndpoint, which a
// client posts to.
func (w *tsWriter) endpointInterface(d goDecl) {
	w.blank()
	jsdoc(&w.body, "", d.doc)
	w.body.WriteString(`export interface AuthEndpoint<Req, Resp> {
  /** The endpoint is a POST to this path, such as /auth/signin. */
  readonly path: string;
  /** Never set: it carries the request and response types to a client. */
  readonly types?: { request: Req; response: Resp };
}
`)
}

func (w *tsWriter) blank() {
	if w.body.Len() > 0 {
		w.body.WriteString("\n")
	}
}

func (w *tsWriter) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated from internal/shared/api/%s.go by make generate; DO NOT EDIT.\n\n", w.module)
	for _, module := range slices.Sorted(maps.Keys(w.imports)) {
		names := slices.Sorted(maps.Keys(w.imports[module]))
		fmt.Fprintf(&b, "import type { %s } from \"./%s\";\n", strings.Join(names, ", "), module)
	}
	if len(w.imports) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(w.body.String())
	return b.String()
}

// jsdoc writes a Go doc comment as a JSDoc comment.
func jsdoc(b *strings.Builder, indent, doc string) {
	doc = strings.ReplaceAll(strings.TrimSpace(doc), "*/", `*\/`)
	if doc == "" {
		return
	}
	lines := strings.Split(doc, "\n")
	if len(lines) == 1 {
		fmt.Fprintf(b, "%s/** %s */\n", indent, doc)
		return
	}
	b.WriteString(indent + "/**\n")
	for _, l := range lines {
		b.WriteString(strings.TrimRight(indent+" * "+l, " ") + "\n")
	}
	b.WriteString(indent + " */\n")
}

// Reading and writing the generated files.

func readTypeScript(dir string) (map[string]string, error) {
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read web/src/api: %w", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read web/src/api: %w", err)
		}
		files[e.Name()] = string(b)
	}
	return files, nil
}

// writeTypeScript makes dir hold exactly the generated files: it is the
// generator's alone.
func writeTypeScript(dir string, files map[string]string) error {
	old, err := readTypeScript(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("make web/src/api: %w", err)
	}
	for name := range old {
		if _, ok := files[name]; !ok {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return fmt.Errorf("remove a stale generated file: %w", err)
			}
		}
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write web/src/api/%s: %w", name, err)
		}
	}
	return nil
}
