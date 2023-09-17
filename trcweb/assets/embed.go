// Package assets contains assets for the trc web interface.
package assets

import "embed"

// FS is the embedded FS of web assets.
//
//go:embed *.css *.html
var FS embed.FS
