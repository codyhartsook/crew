package room

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// Briefing renders what a session should know on arriving in its rooms.
// Returns "" when there is nothing worth injecting.
func Briefing(rooms []Room, entries []*Entry, others []string, authors Authors) string {
	byRoom := group(entries)
	var b strings.Builder

	for _, r := range rooms {
		live := byRoom[r.Key]
		if len(live) == 0 && len(others) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## crew room: %s (%s)\n\n", r.Name, r.Scope)
		if len(others) > 0 && r.Scope == ScopeWorktree {
			fmt.Fprintf(&b, "Also here: %s\n\n", strings.Join(others, ", "))
		}
		writeSection(&b, "Notes", live, ModeNote, authors)
		writeOpen(&b, live, authors)
		writeAnswered(&b, live, authors)
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
	counts := map[Mode]int{}
	answers := 0
	for _, e := range entries {
		switch {
		case e.Resolves != 0:
			answers++
		default:
			counts[e.Mode]++
		}
	}
	var parts []string
	for _, k := range Modes {
		if n := counts[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural(string(k), string(k)+"s", n)))
		}
	}
	if answers > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", answers, plural("answer", "answers", answers)))
	}
	return fmt.Sprintf("crew: %s unread in this room, run `crew room` to read %s.",
		strings.Join(parts, ", "), plural("it", "them", len(entries)))
}

const hint = "Read and acknowledge entries with `crew room`, post with " +
	"`crew post <note|request> \"...\"`, " +
	"answer with `crew resolve <id> \"...\"`."

func writeSection(b *strings.Builder, title string, entries []*Entry, mode Mode, authors Authors) {
	var matching []*Entry
	for _, e := range entries {
		if e.Mode == mode {
			matching = append(matching, e)
		}
	}
	if len(matching) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n", title)
	for _, e := range matching {
		fmt.Fprintf(b, "- [%d] %s - %s, %s\n", e.ID, oneLine(e.Body), authors.Name(e.Author), Ago(e.CreatedAt))
	}
	b.WriteString("\n")
}

// writeOpen lists what is still waiting.
func writeOpen(b *strings.Builder, entries []*Entry, authors Authors) {
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
		to := ""
		if e.To != "" {
			to = " to " + authors.Name(e.To)
		}
		fmt.Fprintf(b, "- [%d] %s%s: %s - %s, %s\n", e.ID, e.Mode, to, oneLine(e.Body), authors.Name(e.Author), Ago(e.CreatedAt))
	}
	b.WriteString("\n")
}

// answeredLimit caps how many resolved threads a briefing carries.
const answeredLimit = 5

// writeAnswered shows resolved threads as question and answer. A closed
// question is no longer waiting on anyone, but its answer is exactly the
// context an arriving agent needs.
func writeAnswered(b *strings.Builder, entries []*Entry, authors Authors) {
	byID := map[int64]*Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}

	var threads []*Entry
	for _, e := range entries {
		if e.Mode.Addressed() && e.Resolves == 0 && e.ResolvedBy != 0 {
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
		to := ""
		if e.To != "" {
			to = " to " + authors.Name(e.To)
		}
		fmt.Fprintf(b, "- [%d] %s%s: %s\n", e.ID, e.Mode, to, oneLine(e.Body))
		if answer, ok := byID[e.ResolvedBy]; ok {
			fmt.Fprintf(b, "  → %s - %s\n", oneLine(answer.Body), authors.Name(answer.Author))
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

// Authors maps immutable session keys to friendly aliases.
type Authors map[string]string

// Name renders a friendly alias, falling back to the session key for old data.
func (a Authors) Name(sessionKey string) string {
	if alias := a[sessionKey]; alias != "" {
		return alias
	}
	return Author(sessionKey)
}

// Display names a session the way a person reads it: its friendly alias when
// it has one, and the session key otherwise.
func Display(s *session.Session) string {
	if s.Alias != "" {
		return s.Alias
	}
	return Author(s.Key())
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
