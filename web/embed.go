package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// FileSystem returns the embedded static file system as an http.FileSystem.
// It panics if the embedded "static" subdirectory is missing, which indicates
// a build-time packaging error.
func FileSystem() http.FileSystem {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(fmt.Sprintf("web: embed sub: %v", err))
	}
	return http.FS(sub)
}
