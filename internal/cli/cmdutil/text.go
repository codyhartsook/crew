package cmdutil

import "strings"

// Truncate collapses whitespace and clips s to n bytes, so a multi-line value
// still fits one table cell.
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
