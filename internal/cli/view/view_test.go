package view

import (
	"io"
	"strings"
	"testing"
)

var cols = []Column{
	{Name: "AGENT"},
	{Name: "FREE"},
	{Name: "SESSION", Human: true},
}

func render(t *testing.T, human bool, rows [][]string) string {
	t.Helper()
	var b strings.Builder
	if err := Table(&b, human, cols, rows); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// An agent never sees a column it cannot act on, header included.
func TestAgentDropsHumanColumns(t *testing.T) {
	got := render(t, false, [][]string{{"coral-lynx", "22%", "01a05f19"}})
	if strings.Contains(got, "SESSION") || strings.Contains(got, "01a05f19") {
		t.Errorf("agent output leaked a human column:\n%s", got)
	}
	for _, want := range []string{"AGENT", "FREE", "coral-lynx", "22%"} {
		if !strings.Contains(got, want) {
			t.Errorf("agent output is missing %q:\n%s", want, got)
		}
	}
}

func TestHumanKeepsEveryColumn(t *testing.T) {
	got := render(t, true, [][]string{{"coral-lynx", "22%", "01a05f19"}})
	for _, want := range []string{"SESSION", "01a05f19", "AGENT", "coral-lynx"} {
		if !strings.Contains(got, want) {
			t.Errorf("human output is missing %q:\n%s", want, got)
		}
	}
}

// A miscounted row must fail loudly: silently shifted columns would put one
// field's value under another's name.
func TestWrongRowWidthIsAnError(t *testing.T) {
	if err := Table(io.Discard, false, cols, [][]string{{"coral-lynx", "22%"}}); err == nil {
		t.Error("a short row should be an error")
	}
	if err := Table(io.Discard, false, cols, [][]string{{"a", "b", "c", "d"}}); err == nil {
		t.Error("a long row should be an error")
	}
}

func TestNoRowsStillWritesHeader(t *testing.T) {
	if got := render(t, false, nil); !strings.Contains(got, "AGENT") {
		t.Errorf("header missing with no rows: %q", got)
	}
}
