// Package view renders tabular output for agents, which pay per token, and people.
// A command declares its columns once and tags Human those an agent cannot act on.
package view

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Column is one field of a row.
type Column struct {
	Name  string
	Human bool
}

// Table writes rows under the columns their audience sees. Cells are in column
// order; a row of the wrong width is an error, not a silently shifted table.
func Table(w io.Writer, human bool, cols []Column, rows [][]string) error {
	keep := make([]int, 0, len(cols))
	names := make([]string, 0, len(cols))
	for i, c := range cols {
		if c.Human && !human {
			continue
		}
		keep = append(keep, i)
		names = append(names, c.Name)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(names, "\t"))
	for n, row := range rows {
		if len(row) != len(cols) {
			return fmt.Errorf("row %d has %d cells, want %d", n, len(row), len(cols))
		}
		cells := make([]string, 0, len(keep))
		for _, i := range keep {
			cells = append(cells, row[i])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}
