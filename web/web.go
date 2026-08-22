// Package web haelt die statische Oberflaeche, die in die Binary eingebettet wird.
package web

import "embed"

//go:embed index.html app.css *.js
var FS embed.FS
