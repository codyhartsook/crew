package room

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Briefing renders what a session should know on arriving in its rooms.
// Returns "" when there is nothing worth injecting.
func Briefing(rooms []Room, entries []*Entry, values []*State, others []string) string {
	byRoomState := map[string][]*State{}
	for _, v := range values {
		byRoomState[v.Room] = append(byRoomState[v.Room], v)
	}
	byRoom := group(entries)
	var b strings.Builder

	for _, r := range rooms {
		live, state := byRoom[r.Key], byRoomState[r.Key]
		if len(live) == 0 && len(state) == 0 && len(others) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## multiplayer room: %s (%s)\n\n", r.Name, r.Scope)
		if len(others) > 0 && r.Scope == ScopeWorktree {
			fmt.Fprintf(&b, "Also here: %s\n\n", strings.Join(others, ", "))
		}
		writeState(&b, state)
		writeProcedures(&b, state)
		writeSection(&b, "Decisions", live, KindDecision)
		writeSection(&b, "Findings", live, KindFinding)
		writeOpen(&b, live)
		writeAnswered(&b, live)
	}

	out := strings.TrimSpace(b.String())
	if out == "" {
		return ""
	}
	return out + "\n\n" + hint
}

// Notice is the one-line nudge a turn hook injects: enough to know something is
// waiting, not the content itself. Reading stays the agent's decision.
func Notice(entries []*Entry) string {
	if len(entries) == 0 {
		return ""
	}
	counts := map[Kind]int{}
	answers := 0
	for _, e := range entries {
		switch {
		case e.Resolves != 0:
			answers++
		default:
			counts[e.Kind]++
		}
	}
	var parts []string
	for _, k := range Kinds {
		if n := counts[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural(string(k), string(k)+"s", n)))
		}
	}
	if answers > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", answers, plural("answer", "answers", answers)))
	}
	return fmt.Sprintf("multiplayer: %s unread in this room — run `multiplayer room --inbox --ack` to read %s.",
		strings.Join(parts, ", "), plural("it", "them", len(entries)))
}

// Delivery renders addressed entries a session has not yet been shown.
func Delivery(entries []*Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## multiplayer: %d new %s\n\n", len(entries), plural("entry", "entries", len(entries)))
	for _, e := range entries {
		switch {
		case e.Resolves != 0:
			fmt.Fprintf(&b, "- [%d] answer to [%d] from %s: %s\n", e.ID, e.Resolves, Author(e.Author), oneLine(e.Body))
		default:
			fmt.Fprintf(&b, "- [%d] %s from %s: %s\n", e.ID, e.Kind, Author(e.Author), oneLine(e.Body))
		}
	}
	return b.String() + "\n" + hint
}

const hint = "Read new entries with `multiplayer room --inbox`, post with " +
	"`multiplayer post <decision|finding|question|handoff|review> \"...\"`, " +
	"answer with `multiplayer resolve <id> \"...\"`."

// stateLimit caps how many values a briefing carries.
const stateLimit = 12

func writeState(b *strings.Builder, values []*State) {
	var facts []*State
	for _, v := range values {
		if !v.IsProcedure() {
			facts = append(facts, v)
		}
	}
	if len(facts) == 0 {
		return
	}
	values = facts
	if len(values) > stateLimit {
		values = values[:stateLimit]
	}
	b.WriteString("### State\n")
	for _, v := range values {
		if v.IsProcedure() {
			continue
		}
		// A long value is named, not reproduced: an arriving agent needs to
		// know a runbook exists, and can read it when it is about to use it.
		if v.Long() {
			fmt.Fprintf(b, "- %s: %s (%d lines) — `multiplayer state get %s`\n",
				v.Key, v.Summary(), v.Lines(), v.Key)
			continue
		}
		fmt.Fprintf(b, "- %s: %s — %s, %s\n", v.Key, oneLine(v.Value), Author(v.Author), Ago(v.UpdatedAt))
	}
	b.WriteString("\n")
}

// writeProcedures lists runbooks separately: an agent scanning for "is there a
// way to do this already" should not have to pick them out of status values.
func writeProcedures(b *strings.Builder, values []*State) {
	var procs []*State
	for _, v := range values {
		if v.IsProcedure() {
			procs = append(procs, v)
		}
	}
	if len(procs) == 0 {
		return
	}
	b.WriteString("### Procedures\n")
	for _, v := range procs {
		fmt.Fprintf(b, "- %s — %s — `multiplayer state get %s`\n",
			strings.TrimPrefix(v.Key, ProcedurePrefix), v.Summary(), v.Key)
	}
	b.WriteString("\n")
}

func writeSection(b *strings.Builder, title string, entries []*Entry, kind Kind) {
	var matching []*Entry
	for _, e := range entries {
		if e.Kind == kind {
			matching = append(matching, e)
		}
	}
	if len(matching) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n", title)
	for _, e := range matching {
		fmt.Fprintf(b, "- [%d] %s — %s, %s\n", e.ID, oneLine(e.Body), Author(e.Author), Ago(e.CreatedAt))
	}
	b.WriteString("\n")
}

// writeOpen lists what is still waiting.
func writeOpen(b *strings.Builder, entries []*Entry) {
	var open []*Entry
	for _, e := range entries {
		if !e.Open() {
			continue
		}
		open = append(open, e)
	}
	if len(open) == 0 {
		return
	}

	b.WriteString("### Open\n")
	for _, e := range open {
		fmt.Fprintf(b, "- [%d] %s: %s — %s, %s\n", e.ID, e.Kind, oneLine(e.Body), Author(e.Author), Ago(e.CreatedAt))
	}
	b.WriteString("\n")
}

// answeredLimit caps how many resolved threads a briefing carries.
const answeredLimit = 5

// writeAnswered shows resolved threads as question and answer. A closed
// question is no longer waiting on anyone, but its answer is exactly the
// context an arriving agent needs.
func writeAnswered(b *strings.Builder, entries []*Entry) {
	byID := map[int64]*Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}

	var threads []*Entry
	for _, e := range entries {
		if e.Kind.Addressed() && e.Resolves == 0 && e.ResolvedBy != 0 {
			threads = append(threads, e)
		}
	}
	if len(threads) == 0 {
		return
	}
	if len(threads) > answeredLimit {
		threads = threads[len(threads)-answeredLimit:]
	}

	b.WriteString("### Answered\n")
	for _, e := range threads {
		fmt.Fprintf(b, "- [%d] %s: %s\n", e.ID, e.Kind, oneLine(e.Body))
		if answer, ok := byID[e.ResolvedBy]; ok {
			fmt.Fprintf(b, "  → %s — %s\n", oneLine(answer.Body), Author(answer.Author))
		}
	}
	b.WriteString("\n")
}

func group(entries []*Entry) map[string][]*Entry {
	byRoom := map[string][]*Entry{}
	for _, e := range entries {
		byRoom[e.Room] = append(byRoom[e.Room], e)
	}
	for _, list := range byRoom {
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	}
	return byRoom
}

// Author renders a session key as "harness 8-char-id".
func Author(sessionKey string) string {
	harness, id, ok := strings.Cut(sessionKey, ":")
	if !ok {
		return sessionKey
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return harness + " " + id
}

// Ago renders elapsed time in the largest readable unit.
func Ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func plural(one, many string, n int) string {
	if n == 1 {
		return one
	}
	return many
}
