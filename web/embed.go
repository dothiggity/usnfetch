// Package webui holds the browser UI (embedded into the binary).
package webui

import "embed"

//go:embed index.html app.css app.js icon-idle.png icon-busy.png icon-paused.png
var FS embed.FS
