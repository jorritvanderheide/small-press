// Command example is a whole site on small-press, for its tests and for a
// new site to start from: its pages (pages.go), templates and stylesheet
// (web/), and content/. A site's own program is this, copied, and changed
// as far as it likes.
package main

import (
	smallpress "codeberg.org/BW20/small-press"
	"codeberg.org/BW20/small-press/example/web"
)

func main() {
	smallpress.Main(smallpress.Site{Pages: pages, Assets: web.Files, Options: sections})
}
