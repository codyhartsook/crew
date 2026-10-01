// Package thread reports a harness's open conversations from its own on-disk
// records, since one process can host many and a pid cannot say which are live.
package thread

import "time"

// Source reports the conversations a harness has open, from its own records.
type Source interface {
	// Open lists open conversations by session id. An error means cannot tell.
	Open() (map[string]Thread, error)
}

// Thread is one conversation a harness has open.
type Thread struct {
	ID         string
	CWD        string
	Source     string    // "user" for a conversation a person drives
	Originator string    // "codex-tui", "Codex Desktop"
	Path       string    // rollout backing it, empty if none found
	Started    time.Time // when the conversation began, zero if unknown
	Active     time.Time // rollout mtime
}
