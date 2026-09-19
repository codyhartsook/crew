package cmdutil

import (
	"encoding/json"
	"io"
	"strings"
)

// Truncate collapses whitespace and clips s to n bytes, so a multi-line value
// still fits one table cell.
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// WriteJSON writes consistently formatted CLI output.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
