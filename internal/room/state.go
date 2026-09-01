package room

import (
	"errors"
	"strings"
	"time"
)

// maxKeyLen bounds a state key.
const maxKeyLen = 200

// State is a keyed, overwritable value in a room.
//
// Entries record that something happened; state records what is currently
// true. Expressing "the migration is half applied" as an append-only chain
// would make every reader fold the history to learn one value, so state is
// written in place and keeps no history of its own - if the change itself
// matters, post a decision alongside it.
type State struct {
	Room  string `json:"room"`
	Scope Scope  `json:"scope"`
	Key   string `json:"key"`
	Value string `json:"value"`
	// Author is whoever wrote it last.
	Author    string    `json:"author"`
	Revision  int       `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

// longValue is where a value stops being a fact and starts being a document.
const longValue = 120

// Long reports whether the value is too big to inline in a briefing. A runbook
// belongs in state - it is keyed, named and replaced when the process changes -
// but pushing it whole at every arriving agent would cost more context than it
// is worth.
func (s *State) Long() bool {
	return strings.Contains(s.Value, "\n") || len(s.Value) > longValue
}

// Summary is the first line of the value, for a briefing that lists a long
// value rather than reproducing it.
func (s *State) Summary() string {
	first, _, _ := strings.Cut(s.Value, "\n")
	first = strings.TrimSpace(first)
	if len(first) > longValue {
		first = first[:longValue] + "…"
	}
	return first
}

// Lines counts the lines in the value.
func (s *State) Lines() int {
	if s.Value == "" {
		return 0
	}
	return strings.Count(s.Value, "\n") + 1
}

// ProcedurePrefix marks a value that is a runbook rather than a fact.
const ProcedurePrefix = "procedure/"

// IsProcedure reports whether the value is a runbook.
func (s *State) IsProcedure() bool { return strings.HasPrefix(s.Key, ProcedurePrefix) }

// Query is a free-text search over a room.
type Query struct {
	Text  string
	Rooms []string
	Limit int
}

// Results are what a search found, state first: a runbook answers "how do I do
// this" more directly than the entry that happened to mention it.
type Results struct {
	State   []*State `json:"state"`
	Entries []*Entry `json:"entries"`
}

// Empty reports whether the search found nothing.
func (r *Results) Empty() bool { return len(r.State) == 0 && len(r.Entries) == 0 }

// StateFilter narrows a state listing.
type StateFilter struct {
	Rooms []string
	// Prefix matches the start of the key, which is how keys namespace
	// themselves ("build/", "migration/") without a column for it.
	Prefix string
	Limit  int
}

// ValidKey rejects keys that would be unusable as identifiers.
func ValidKey(key string) error {
	switch {
	case strings.TrimSpace(key) == "":
		return errors.New("key is empty")
	case len(key) > maxKeyLen:
		return errors.New("key is too long")
	case strings.ContainsAny(key, " \t\n"):
		return errors.New("key cannot contain whitespace")
	}
	return nil
}
