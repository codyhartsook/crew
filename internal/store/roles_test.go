package store_test

import (
	"strings"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/store"
)

func TestRoleValidate(t *testing.T) {
	ok := "Runs the suite: send finished changes."
	cases := []struct {
		name, role, desc string
		valid            bool
	}{
		{"plain", "tester", ok, true},
		{"dashes and digits", "qa-2", ok, true},
		{"at the name limit", strings.Repeat("a", 32), ok, true},
		{"at the description limit", "tester", strings.Repeat("x", 300), true},
		{"uppercase", "Tester", ok, false},
		{"leading digit", "2qa", ok, false},
		{"space", "test er", ok, false},
		{"empty name", "", ok, false},
		{"name too long", strings.Repeat("a", 33), ok, false},
		{"no description", "tester", "  ", false},
		{"two lines", "tester", "one\ntwo", false},
		{"description too long", "tester", strings.Repeat("x", 301), false},
	}
	for _, tc := range cases {
		err := (&store.Role{Name: tc.role, Description: tc.desc}).Validate()
		if (err == nil) != tc.valid {
			t.Errorf("%s: Validate = %v, want valid=%v", tc.name, err, tc.valid)
		}
	}
}
