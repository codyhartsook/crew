// Package ui serves the read-only registry web interface: one self-contained
// HTML file compiled into the binary, reading the same /v1/sessions endpoint
// any other client would.
package ui

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var indexHTML []byte

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The page polls for its own data, but the page itself changes whenever
		// the binary is rebuilt, so it must not be cached.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(indexHTML)
	})
}
