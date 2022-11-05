package assets

import (
	"embed"
)

//go:embed *.html *.css
var Embedded embed.FS
