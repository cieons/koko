// Package notices holds only the test that writes, and checks, the
// third-party notices koko and kokod embed and print with version
// --licenses (#220): every component each binary carries, with the licence
// and notices it is distributed under, read from the module cache, the web
// UI's node_modules and the files vendored in the repository. It fails when
// a component's licence is not one koko accepts (acceptedLicences), and when
// a committed notices file differs from what it would write; go test
// ./internal/notices -update rewrites them.
package notices

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/install"
)

var update = flag.Bool("update", false, "rewrite the THIRD_PARTY_NOTICES files koko and kokod embed")

// root is the repository's root, from this package's directory.
const root = "../.."

// A program is a binary a release ships, with the notices file it embeds.
type program struct {
	name string
	// pkg is its main package.
	pkg string
	// platforms are the GOOS/GOARCH it is released for, as the Makefile's
	// RELEASE_TARGETS and KOKOD_TARGETS list them; its notices cover what
	// any of them links.
	platforms []string
	// notices is the file it embeds, from the repository's root.
	notices string
}

var programs = []program{
	{
		name:      "koko",
		pkg:       "./cmd/koko",
		platforms: []string{"linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64"},
		notices:   "internal/client/cli/THIRD_PARTY_NOTICES",
	},
	{
		name:      "kokod",
		pkg:       "./cmd/kokod",
		platforms: install.KokodPlatforms,
		notices:   "internal/server/kokod/THIRD_PARTY_NOTICES",
	},
}

// acceptedLicences are the licences, as SPDX identifiers, that a component
// of a koko binary may carry: permissive ones, and MPL-2.0, whose files
// stay apart. A copyleft licence such as the GPL is not among them.
var acceptedLicences = []string{
	"0BSD", "Apache-2.0", "BlueOak-1.0.0", "BSD-2-Clause", "BSD-3-Clause",
	"CC0-1.0", "ISC", "MIT", "MPL-2.0", "OFL-1.1", "Unlicense", "Zlib",
}

// A component is one piece of third-party software a binary carries.
type component struct {
	// name says what it is, such as "golang.org/x/sys v0.49.0".
	name string
	// licence is its licence as an SPDX identifier or expression.
	licence string
	// texts are its licence and notice files, as notices prints them.
	texts []string
}

// The files vendored in the repository, each with the package that embeds
// it and the licence beside it.
var vendored = []struct {
	name, pkg, licence string
}{
	{
		name:    "Ghostty's VT module (libghostty-vt), as the web terminal's emulator in kokod and in the web UI's @wterm/ghostty",
		pkg:     "github.com/cieons/koko/internal/server/vt",
		licence: "internal/server/vt/ghostty-vt.wasm.LICENSE",
	},
	{
		name:    "Geist, the web UI's font",
		pkg:     "github.com/cieons/koko/internal/server/webui",
		licence: "web/src/assets/fonts/Geist-OFL.txt",
	},
	{
		name:    "Geist Mono, the web UI's monospaced font",
		pkg:     "github.com/cieons/koko/internal/server/webui",
		licence: "web/src/assets/fonts/GeistMono-OFL.txt",
	},
}

// webUI is the package that embeds the web UI's bundle, whose npm packages
// a binary that links it carries.
const webUI = "github.com/cieons/koko/internal/server/webui"

func TestTheNoticesAreUpToDate(t *testing.T) {
	for _, p := range programs {
		t.Run(p.name, func(t *testing.T) {
			components, err := p.components(t)
			if err != nil {
				t.Fatal(err)
			}
			want := render(p.name, components)
			file := filepath.Join(root, p.notices)
			if *update {
				if err := os.WriteFile(file, []byte(want), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", p.notices, err)
			}
			if string(got) != want {
				t.Errorf("%s is out of date with %s's dependencies: run go test ./internal/notices -update", p.notices, p.name)
			}
		})
	}
}

// The notices name what the issue asks for: wazero with its NOTICE in
// kokod's, Ghostty's VT module in kokod's, and the Go project's licence in
// both.
func TestTheNoticesNameWhatEachBinaryCarries(t *testing.T) {
	read := func(file string) string {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	kokod := read(programs[1].notices)
	for _, want := range []string{
		"github.com/tetratelabs/wazero v",
		"wazero\nCopyright 2020-2023 wazero authors", // its NOTICE
		"Ghostty's VT module",
		"Mitchell Hashimoto, Ghostty contributors",
		"modernc.org/sqlite v",
		"npm:react 19.",
		"Geist, the web UI's font",
		"Go standard library and runtime",
	} {
		if !strings.Contains(kokod, want) {
			t.Errorf("kokod's notices do not contain %q", want)
		}
	}
	koko := read(programs[0].notices)
	for _, want := range []string{"golang.org/x/crypto v", "Go standard library and runtime"} {
		if !strings.Contains(koko, want) {
			t.Errorf("koko's notices do not contain %q", want)
		}
	}
	for _, unwanted := range []string{"wazero", "npm:", "modernc.org"} {
		if strings.Contains(koko, unwanted) {
			t.Errorf("koko's notices contain %q, which koko does not carry", unwanted)
		}
	}
}

// A licence outside acceptedLicences fails the check, whether a Go module's
// file says it or an npm package's metadata does.
func TestALicenceOutsideTheAcceptedOnesFails(t *testing.T) {
	texts := map[string]string{
		"GPL-3.0":       "GNU GENERAL PUBLIC LICENSE\nVersion 3, 29 June 2007\n",
		"LGPL-2.1":      "GNU LESSER GENERAL PUBLIC LICENSE\nVersion 2.1, February 1999\n",
		"AGPL-3.0":      "GNU AFFERO GENERAL PUBLIC LICENSE\nVersion 3, 19 November 2007\n",
		"unrecognised":  "All rights reserved.\n",
		"MIT, then GPL": mitText + "\nThis file is also licensed under the GNU General Public License.\n",
	}
	for name, text := range texts {
		if id, err := classify(text); err == nil {
			t.Errorf("classify(%s) = %s, not refused", name, id)
		}
	}
	for _, expr := range []string{"GPL-3.0", "GPL-2.0-or-later", "MIT AND GPL-3.0", "(MIT OR GPL-3.0) AND LGPL-2.1", "SEE LICENSE IN LICENSE", ""} {
		if err := checkExpression(expr); err == nil {
			t.Errorf("checkExpression(%q) accepted it", expr)
		}
	}
	for _, expr := range []string{"MIT", "(MIT OR GPL-3.0)", "Apache-2.0 AND MIT", "(BSD-3-Clause OR MIT)"} {
		if err := checkExpression(expr); err != nil {
			t.Errorf("checkExpression(%q): %v", expr, err)
		}
	}
	for want, text := range map[string]string{
		"MIT":          mitText,
		"BSD-3-Clause": "Redistribution and use in source and binary forms, with or without modification, are permitted. Neither the name of the copyright holder nor the names of its contributors may be used to endorse",
		"BSD-2-Clause": "Redistribution and use in source and binary forms, with or without modification, are permitted provided that",
		"ISC":          "Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies.",
		"Apache-2.0":   "Apache License\n Version 2.0, January 2004",
	} {
		if got, err := classify(text); err != nil || got != want {
			t.Errorf("classify(%s) = %q, %v", want, got, err)
		}
	}
}

const mitText = "Permission is hereby granted, free of charge, to any person obtaining a copy of this software"

// components lists every third-party component p carries: the Go project's
// own code, every module its main package links on any of its platforms,
// the vendored files a linked package embeds, and, when it links the web
// UI, the npm packages the bundle's build depends on.
func (p program) components(t *testing.T) ([]component, error) {
	modules := map[string]goModule{}
	linked := map[string]bool{}
	for _, platform := range p.platforms {
		pkgs, err := goList(t, p.pkg, platform)
		if err != nil {
			return nil, err
		}
		for _, pkg := range pkgs {
			linked[pkg.ImportPath] = true
			if m := pkg.Module; m != nil && !m.Main {
				modules[m.Path] = *m
			}
		}
	}
	sys, ok := modules["golang.org/x/sys"]
	if !ok {
		return nil, fmt.Errorf("%s does not link golang.org/x/sys, whose files are the Go project's licence and patent grant", p.name)
	}
	// Go's own licence and patent grant, which cover the standard library
	// and runtime in every binary, are the files golang.org/x/sys carries,
	// word for word: a GOROOT need not hold them (Homebrew's has neither).
	goProject, err := moduleComponent(sys)
	if err != nil {
		return nil, err
	}
	goProject.name = "Go standard library and runtime"
	components := []component{goProject}
	for _, m := range slices.SortedFunc(maps.Values(modules), func(a, b goModule) int { return cmp.Compare(a.Path, b.Path) }) {
		c, err := moduleComponent(m)
		if err != nil {
			return nil, err
		}
		components = append(components, c)
	}
	for _, v := range vendored {
		if !linked[v.pkg] {
			continue
		}
		c, err := fileComponent(v.name, filepath.Join(root, v.licence))
		if err != nil {
			return nil, err
		}
		components = append(components, c)
	}
	if linked[webUI] {
		npm, err := npmComponents()
		if err != nil {
			return nil, err
		}
		components = append(components, npm...)
	}
	return components, nil
}

// goModule is a module as go list reports it.
type goModule struct {
	Path    string
	Version string
	Dir     string
	Main    bool
}

type goPackage struct {
	ImportPath string
	Module     *goModule
}

// goList lists pkg's dependencies, itself included, as the build for
// platform links them.
func goList(t *testing.T, pkg, platform string) ([]goPackage, error) {
	goos, goarch, _ := strings.Cut(platform, "/")
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-json=ImportPath,Module", pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s for %s: %w\n%s", pkg, platform, err, stderr.String())
	}
	dec := jsontext.NewDecoder(bytes.NewReader(out))
	var pkgs []goPackage
	for {
		var p goPackage
		if err := json.UnmarshalDecode(dec, &p); err != nil {
			if errors.Is(err, io.EOF) {
				return pkgs, nil
			}
			return nil, fmt.Errorf("read go list's output for %s: %w", platform, err)
		}
		pkgs = append(pkgs, p)
	}
}

// licenceFile matches the files at a module's or package's root that carry
// its licence or notices; primaryLicence those of them that are its
// licence itself, which classify reads. The others, such as NOTICE,
// PATENTS or LICENSE-3RD-PARTY.md, are printed as they are.
var (
	licenceFile    = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|patents)([.-].*)?$`)
	primaryLicence = regexp.MustCompile(`(?i)^(licen[cs]e|copying)(\.(txt|md))?$`)
)

// moduleComponent reads a Go module's licence and notices from the module
// cache, and classifies its licence.
func moduleComponent(m goModule) (component, error) {
	c := component{name: m.Path + " " + m.Version}
	files, err := licenceFiles(m.Dir)
	if err != nil {
		return component{}, fmt.Errorf("%s: %w", c.name, err)
	}
	var ids []string
	for _, f := range files {
		if !primaryLicence.MatchString(f.name) {
			continue
		}
		id, err := classify(f.text)
		if err != nil {
			return component{}, fmt.Errorf("%s's %s: %w", c.name, f.name, err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return component{}, fmt.Errorf("%s carries no licence file at its root", c.name)
	}
	slices.Sort(ids)
	c.licence = strings.Join(slices.Compact(ids), " AND ")
	for _, f := range files {
		c.texts = append(c.texts, f.text)
	}
	return c, nil
}

// fileComponent is a vendored file's component, from the licence beside it.
func fileComponent(name, licence string) (component, error) {
	b, err := os.ReadFile(licence)
	if err != nil {
		return component{}, fmt.Errorf("%s: %w", name, err)
	}
	text := normalise(string(b))
	id, err := classify(text)
	if err != nil {
		return component{}, fmt.Errorf("%s's %s: %w", name, licence, err)
	}
	return component{name: name, licence: id, texts: []string{text}}, nil
}

type licenceText struct {
	name, text string
}

// licenceFiles reads the licence and notice files at dir's root, in name
// order.
func licenceFiles(dir string) ([]licenceText, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []licenceText
	for _, e := range entries {
		if e.IsDir() || !licenceFile.MatchString(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, licenceText{name: e.Name(), text: normalise(string(b))})
	}
	return files, nil
}

// npmComponents lists the npm packages of the web UI's build that are not
// for development alone, as web/package-lock.json marks them, read from
// web/node_modules: what the bundle can hold. Tailwind CSS's base styles,
// for one, go into it, and @heroui/styles depends on it.
func npmComponents() ([]component, error) {
	web := filepath.Join(root, "web")
	b, err := os.ReadFile(filepath.Join(web, "package-lock.json"))
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages map[string]struct {
			Version     string `json:"version"`
			Dev         bool   `json:"dev"`
			DevOptional bool   `json:"devOptional"`
			Optional    bool   `json:"optional"`
			Link        bool   `json:"link"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(b, &lock); err != nil {
		return nil, fmt.Errorf("read web/package-lock.json: %w", err)
	}
	var components []component
	for _, key := range slices.Sorted(maps.Keys(lock.Packages)) {
		entry := lock.Packages[key]
		if key == "" || entry.Dev || entry.DevOptional || entry.Link {
			continue
		}
		dir := filepath.Join(web, filepath.FromSlash(key))
		_, name, _ := strings.CutLast(key, "node_modules/")
		manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if errors.Is(err, os.ErrNotExist) && entry.Optional {
			continue // an optional package for another platform
		}
		if err != nil {
			return nil, fmt.Errorf("npm package %s, which the web UI's build needs: %w (run make webui, whose npm ci installs it)", name, err)
		}
		var pkg struct {
			License string `json:"license"`
		}
		if err := json.Unmarshal(manifest, &pkg); err != nil {
			return nil, fmt.Errorf("read %s's package.json: %w", name, err)
		}
		if err := checkExpression(pkg.License); err != nil {
			return nil, fmt.Errorf("npm package %s %s: %w", name, entry.Version, err)
		}
		files, err := licenceFiles(dir)
		if err != nil {
			return nil, fmt.Errorf("npm package %s: %w", name, err)
		}
		c := component{name: "npm:" + name + " " + entry.Version, licence: pkg.License}
		for _, f := range files {
			if primaryLicence.MatchString(f.name) && refused(f.text) {
				return nil, fmt.Errorf("npm package %s's %s names a copyleft or source-available licence, which koko does not accept", name, f.name)
			}
			c.texts = append(c.texts, f.text)
		}
		components = append(components, c)
	}
	return components, nil
}

// refusedMarkers are phrases of licences koko does not accept, which no
// accepted licence's text holds.
var refusedMarkers = []string{
	"gnu general public license",
	"gnu lesser general public license",
	"gnu library general public license",
	"gnu affero general public license",
	"server side public license",
	"business source license",
	"commons clause",
}

// refused reports whether text names a licence koko does not accept.
func refused(text string) bool {
	flat := flatten(text)
	return slices.ContainsFunc(refusedMarkers, func(m string) bool { return strings.Contains(flat, m) })
}

// classify names the licence text grants, as an SPDX identifier, and fails
// for one not in acceptedLicences or not recognised.
func classify(text string) (string, error) {
	id, err := recognise(text)
	if err != nil {
		return "", err
	}
	if !slices.Contains(acceptedLicences, id) {
		return "", fmt.Errorf("is %s, which koko does not accept (accepted: %s)", id, strings.Join(acceptedLicences, ", "))
	}
	return id, nil
}

// recognise names the licence text grants, as an SPDX identifier.
func recognise(text string) (string, error) {
	if refused(text) {
		return "", errors.New("names a copyleft or source-available licence, which koko does not accept")
	}
	flat := flatten(text)
	has := func(phrases ...string) bool {
		return !slices.ContainsFunc(phrases, func(p string) bool { return !strings.Contains(flat, p) })
	}
	switch {
	case has("apache license", "version 2.0"):
		return "Apache-2.0", nil
	case has("mozilla public license", "2.0"):
		return "MPL-2.0", nil
	case has("permission is hereby granted, free of charge"):
		return "MIT", nil
	case has("redistribution and use in source and binary forms"):
		if has("neither the name") || has("may be used to endorse") {
			return "BSD-3-Clause", nil
		}
		return "BSD-2-Clause", nil
	case has("permission to use, copy, modify, and/or distribute this software for any purpose"),
		has("permission to use, copy, modify, and distribute this software for any purpose"):
		if has("copyright notice and this permission notice appear in all copies") {
			return "ISC", nil
		}
		return "0BSD", nil
	case has("sil open font license", "version 1.1"):
		return "OFL-1.1", nil
	}
	return "", errors.New("is not a licence text koko recognises: read it, then teach recognise to name it")
}

// checkExpression fails unless the SPDX expression expr is satisfied by
// accepted licences alone: one alternative of an OR, every part of an AND,
// and every part when it mixes the two.
func checkExpression(expr string) error {
	ids := strings.Fields(strings.NewReplacer("(", " ", ")", " ").Replace(expr))
	if len(ids) == 0 {
		return errors.New("declares no licence")
	}
	var alternatives [][]string
	current := []string{}
	mixed := slices.Contains(ids, "AND") && slices.Contains(ids, "OR")
	for _, id := range ids {
		switch id {
		case "OR":
			if !mixed {
				alternatives = append(alternatives, current)
				current = []string{}
			}
		case "AND":
		default:
			current = append(current, id)
		}
	}
	alternatives = append(alternatives, current)
	for _, alt := range alternatives {
		if !slices.ContainsFunc(alt, func(id string) bool { return !slices.Contains(acceptedLicences, id) }) {
			return nil
		}
	}
	return fmt.Errorf("declares the licence %q, which koko does not accept (accepted: %s)", expr, strings.Join(acceptedLicences, ", "))
}

// flatten lower-cases text and collapses its white space, so that phrases
// match across line breaks.
func flatten(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

// normalise ends every line without trailing space and the text with one
// newline, so that a notices file reads the same from any checkout.
func normalise(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n") + "\n"
}

const rule = "================================================================================"

// render writes program's notices: one part for each licence and set of
// texts, naming the components it covers, so that the many packages that
// share one licence file print it once.
func render(program string, components []component) string {
	type part struct {
		licence string
		names   []string
		texts   []string
	}
	parts := map[string]*part{}
	for _, c := range components {
		key := c.licence + "\x00" + strings.Join(c.texts, "\x00")
		p, ok := parts[key]
		if !ok {
			p = &part{licence: c.licence, texts: c.texts}
			parts[key] = p
		}
		p.names = append(p.names, c.name)
	}
	ordered := slices.Collect(maps.Values(parts))
	for _, p := range ordered {
		slices.Sort(p.names)
	}
	slices.SortFunc(ordered, func(a, b *part) int { return cmp.Compare(a.names[0], b.names[0]) })

	var b strings.Builder
	fmt.Fprintf(&b, "Third-party notices for %s\n\n", program)
	fmt.Fprintf(&b, "%s includes the third-party software below. Each part names the\n", program)
	b.WriteString("components it covers, then the licence and notices they are distributed\nunder.\n")
	for _, p := range ordered {
		fmt.Fprintf(&b, "\n%s\n\nLicence: %s\n\n", rule, p.licence)
		for _, n := range p.names {
			fmt.Fprintf(&b, "  %s\n", n)
		}
		if len(p.texts) == 0 {
			b.WriteString("\nThe package carries no licence file; its metadata names the licence above.\n")
		}
		for i, text := range p.texts {
			if i > 0 {
				fmt.Fprintf(&b, "\n%s\n", strings.Repeat("-", len(rule)))
			}
			b.WriteString("\n" + text)
		}
	}
	return b.String()
}
