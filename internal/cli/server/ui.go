package server

// Each page is one self-contained HTML file compiled into the binary, reading
// the same /v1/sessions endpoint any other client would.

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var indexHTML []byte

//go:embed crew.html
var crewHTML []byte

// dashboardPage serves the dashboard: a force-directed graph of where agents are
// working, home at the centre and one ring per depth.
func dashboardPage() http.Handler {
	return page(crewHTML)
}

// tablePage serves the older row-per-session view at /table. The ring is better
// for noticing, a table is better for reading, and the table costs nothing to
// keep.
func tablePage() http.Handler {
	return page(indexHTML)
}

func page(body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The page polls for its own data, but the page itself changes whenever
		// the binary is rebuilt, so it must not be cached.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	})
}
