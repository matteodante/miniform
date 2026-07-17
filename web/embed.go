package web

import (
	"embed"
	"io/fs"
)

//go:embed templates
var embeddedTemplates embed.FS

//go:embed static
var embeddedStatic embed.FS

var (
	Templates = subdirectory(embeddedTemplates, "templates")
	Static    = subdirectory(embeddedStatic, "static")
)

func subdirectory(files embed.FS, name string) fs.FS {
	root, err := fs.Sub(files, name)
	if err != nil {
		panic("web: embedded " + name + " unavailable: " + err.Error())
	}
	return root
}
