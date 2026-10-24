package web

import "embed"

// Files is embedded into the server so deployment needs only one executable.
//
//go:embed index.html app.js history.js styles.css editor-bundle.js
var Files embed.FS
