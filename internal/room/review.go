package room

import "time"

// Severity ranks how much a finding matters.
type Severity string

const (
	SeverityMust     Severity = "must"
	SeverityShould   Severity = "should"
	SeverityConsider Severity = "consider"
)

// Severities lists every severity, most serious first.
var Severities = []Severity{SeverityMust, SeverityShould, SeverityConsider}

func (s Severity) Valid() bool {
	for _, known := range Severities {
		if s == known {
			return true
		}
	}
	return false
}

// Anchor locates a finding in the code.
//
// Line rots the moment the author edits, so Symbol and BlobSHA are what let a
// finding be relocated through a diff later. Keeping the anchor out of the
// finding's prose is also what would make promoting it to a pull-request
// comment possible.
type Anchor struct {
	File    string `json:"file"`
	Symbol  string `json:"symbol,omitempty"`
	Line    int    `json:"line,omitempty"`
	BlobSHA string `json:"blob_sha,omitempty"`
}

// Empty reports whether the anchor points at nothing.
func (a *Anchor) Empty() bool {
	return a == nil || (a.File == "" && a.Symbol == "" && a.Line == 0)
}

// Ref renders the anchor as file:line, or file:symbol when the line is unknown.
func (a *Anchor) Ref() string {
	if a == nil || a.File == "" {
		return ""
	}
	switch {
	case a.Line > 0:
		return a.File + ":" + itoa(a.Line)
	case a.Symbol != "":
		return a.File + ":" + a.Symbol
	default:
		return a.File
	}
}

// Review is one batch of findings over a named target.
//
// A batch is not itself an entry: its findings are, so a review with thirty
// findings does not become thirty things a briefing must list.
type Review struct {
	ID     int64  `json:"id"`
	Room   string `json:"room"`
	Author string `json:"author"`
	// Target names what was reviewed - a ref range, a path, a description.
	Target    string    `json:"target,omitempty"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`

	// Findings and Open are filled in on read.
	Findings int `json:"findings"`
	Open     int `json:"open"`
}

// Label is how a review is named in output.
func (r *Review) Label() string { return "r" + itoa64(r.ID) }

// ReviewFilter narrows a review listing.
type ReviewFilter struct {
	IDs      []int64
	Rooms    []string
	OpenOnly bool
	Limit    int
}

func itoa(n int) string { return itoa64(int64(n)) }

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
