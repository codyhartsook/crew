package pickcmd

import "testing"

func names(cs []candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.name)
	}
	return out
}

func equal(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSortByHeadroom(t *testing.T) {
	cs := []candidate{
		{name: "busy", headroom: 0.10, multiplier: 1},
		{name: "free", headroom: 0.90, multiplier: 5},
		{name: "mid", headroom: 0.50, multiplier: 5},
	}
	sortBy(cs, false)
	if got := names(cs); !equal(got, "free", "mid", "busy") {
		t.Errorf("order = %v, want free mid busy", got)
	}
}

func TestSortByCheapest(t *testing.T) {
	cs := []candidate{
		{name: "opus", headroom: 0.90, multiplier: 5},
		{name: "haiku", headroom: 0.10, multiplier: 1},
		{name: "sonnet", headroom: 0.50, multiplier: 2},
	}
	sortBy(cs, true)
	if got := names(cs); !equal(got, "haiku", "sonnet", "opus") {
		t.Errorf("order = %v, want haiku sonnet opus", got)
	}
}

// Two agents on the same model are separated by their free context, so the
// secondary key has to actually apply.
func TestCheapestBreaksTiesOnHeadroom(t *testing.T) {
	cs := []candidate{
		{name: "tight", headroom: 0.20, multiplier: 5},
		{name: "roomy", headroom: 0.80, multiplier: 5},
	}
	sortBy(cs, true)
	if got := names(cs); !equal(got, "roomy", "tight") {
		t.Errorf("order = %v, want roomy tight", got)
	}
}

// An unpriced model must never win on price: not knowing what it costs is no
// evidence that it is cheap. It still competes normally on free context.
func TestUnpricedNeverWinsOnPrice(t *testing.T) {
	cs := []candidate{
		{name: "unpriced", headroom: 0.50},
		{name: "opus", headroom: 0.40, multiplier: 5},
	}
	sortBy(cs, true)
	if got := names(cs); !equal(got, "opus", "unpriced") {
		t.Errorf("cheapest order = %v, want opus unpriced", got)
	}

	sortBy(cs, false)
	if got := names(cs); !equal(got, "unpriced", "opus") {
		t.Errorf("headroom order = %v, want unpriced opus", got)
	}
}

func TestCheaper(t *testing.T) {
	cases := []struct {
		name string
		a, b float64
		want bool
	}{
		{"lower price wins", 1, 5, true},
		{"higher price loses", 5, 1, false},
		{"unknown loses to priced", 0, 5, false},
		{"priced beats unknown", 5, 0, true},
		{"both unknown", 0, 0, false},
		{"equal", 5, 5, false},
	}
	for _, c := range cases {
		if got := cheaper(c.a, c.b); got != c.want {
			t.Errorf("%s: cheaper(%v, %v) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}
