// Package webui is the web UI the koko binary embeds (ADR 0023): the React
// app in web/, which make builds into site/app, or, in a binary built without
// it (a plain go build or go install), one placeholder page that says how to
// build it, for the koko server to serve on web@koko.
//
// It is server-only: no client package imports it.
package webui

import (
	"embed"
	"fmt"
	"io/fs"
)

// site holds placeholder/index.html, which is committed, and app/, which
// Vite writes (web/vite.config.ts) and git ignores. The all: prefix keeps
// files whose names begin with '.' or '_', which a bundler may emit.
//
//go:embed all:site
var site embed.FS

var files, built = pick(site)

// FS returns the files the web UI is served from, with index.html at the
// root: the built app, or the placeholder page alone.
func FS() fs.FS {
	return files
}

// Built reports whether this binary carries the built web UI rather than the
// placeholder page.
func Built() bool {
	return built
}

// pick chooses the built app when site holds one, and the placeholder when it
// does not.
func pick(site fs.FS) (fs.FS, bool) {
	app := sub(site, "site/app")
	if _, err := fs.Stat(app, "index.html"); err == nil {
		return app, true
	}
	return sub(site, "site/placeholder"), false
}

// sub is fs.Sub for a constant directory name, which it never refuses.
func sub(fsys fs.FS, dir string) fs.FS {
	s, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(fmt.Sprintf("webui: %v", err))
	}
	return s
}
