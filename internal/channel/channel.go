// Package channel is the directed back-channel: one agent asks another for
// context it was not handed, and blocks until it is answered. Not the room,
// which is a broadcast every member reads.
package channel

import (
	"fmt"
	"time"
)

// Addr is a session key, "<harness>:<id>", as room entries already use.
type Addr string

// Request is one question and its answer. State is derived: no AnsweredAt
// means open.
type Request struct {
	ID         int64      `json:"id"`
	From       Addr       `json:"from"`
	To         Addr       `json:"to"`
	Body       string     `json:"body"`
	Answer     string     `json:"answer,omitempty"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Asker is the address a delegated role asks from: its session key once its
// SessionStart hook recorded one, else the delegation, so a sender is always
// nameable.
func Asker(child, delegationID string) Addr {
	if child != "" {
		return Addr(child)
	}
	return Addr("delegation:" + delegationID)
}

// Open reports whether the request still wants an answer.
func (r *Request) Open() bool { return r != nil && r.AnsweredAt == nil }

// Notice names what is waiting and how to answer it. The question is included
// rather than fetched: it is one sentence, and a round trip costs a turn.
func Notice(open []*Request) string {
	if len(open) == 0 {
		return ""
	}
	newest := open[len(open)-1]
	if len(open) == 1 {
		return fmt.Sprintf("crew: a delegated role asks [%d]: %s\nAnswer it with: crew answer %d \"<body>\"",
			newest.ID, newest.Body, newest.ID)
	}
	return fmt.Sprintf("crew: %d delegated roles are waiting on you, newest [%d]: %s\nAnswer each with: crew answer <id> \"<body>\"",
		len(open), newest.ID, newest.Body)
}
