// Package web embeds the built management UI.
//
// The dist directory is produced by `make ui` from the frontend sources in
// /web; a source checkout only has the placeholder, so Dist reports whether a
// real build is present rather than failing the whole binary.
package web

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// ErrNoUI means this binary was built without a UI bundle.
var ErrNoUI = errors.New("web: no UI bundle embedded")

func Dist() (fs.FS, error) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, ErrNoUI
	}
	return sub, nil
}
