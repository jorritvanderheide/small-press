// Package web holds the templates and the stylesheet, embedded in the
// program. In development they are read from disk instead (-dev-assets), so
// an edit shows at once.
package web

import "embed"

//go:embed templates static
var Files embed.FS
