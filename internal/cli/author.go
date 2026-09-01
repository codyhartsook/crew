package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/proc"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// sessionKeysFromEnv lists every identity the environment advertises. A harness
// puts its session id in the environment of the commands it runs, which is exact
// and, unlike process inspection, survives a sandbox. A harness launched from
// inside another inherits its variables, so the first one found is not
// necessarily ours.
func sessionKeysFromEnv() []string {
	var keys []string
	seen := map[string]bool{}
	for _, spec := range harness.Specs() {
		for _, env := range spec.SessionEnv {
			id := os.Getenv(env)
			if id == "" {
				continue
			}
			key := string(spec.Harness) + ":" + id
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// resolveAuthor works out which registered session is running this command,
// trying the cheapest and most certain source first.
func resolveAuthor(ctx context.Context, st store.Store, roomKeys []string) (string, error) {
	active, err := st.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return "", err
	}
	known := map[string]bool{}
	byPID := map[int][]*session.Session{}
	var here []*session.Session
	for _, s := range active {
		known[s.Key()] = true
		if s.PID != 0 {
			byPID[s.PID] = append(byPID[s.PID], s)
		}
		if s.Repo != nil && contains(roomKeys, s.Repo.Root) {
			here = append(here, s)
		}
	}
	inRoom := map[string]bool{}
	for _, s := range here {
		inRoom[s.Key()] = true
	}

	// 1. The harness told us who we are. Where a nested harness offers several
	//    identities, keep the ones the registry recognises and, if that is
	//    still ambiguous, the one in this room.
	if matches := filterKnown(sessionKeysFromEnv(), known); len(matches) > 0 {
		if len(matches) == 1 {
			return matches[0], nil
		}
		if narrowed := filterHere(matches, here); len(narrowed) == 1 {
			return narrowed[0], nil
		}
	}
	// 2. An ancestor process is a registered harness. Unavailable behind a
	//    sandbox. A pid claimed by two sessions proves nothing, and a match
	//    outside this room is likely coincidence: guessing wrong files an entry
	//    under another agent's name.
	if table, err := proc.Snapshot(); err == nil {
		for _, pid := range table.Ancestors(os.Getpid()) {
			candidates := byPID[pid]
			if len(candidates) != 1 || !inRoom[candidates[0].Key()] {
				continue
			}
			return candidates[0].Key(), nil
		}
	}
	// 3. Only one agent is here, so it must be the caller.
	if len(here) == 1 {
		return here[0].Key(), nil
	}
	return "", ambiguous(here)
}

func filterKnown(keys []string, known map[string]bool) []string {
	var out []string
	for _, key := range keys {
		if known[key] {
			out = append(out, key)
		}
	}
	return out
}

func filterHere(keys []string, here []*session.Session) []string {
	inRoom := map[string]bool{}
	for _, s := range here {
		inRoom[s.Key()] = true
	}
	var out []string
	for _, key := range keys {
		if inRoom[key] {
			out = append(out, key)
		}
	}
	return out
}

// ambiguous explains the failure and names the candidates, so the caller can
// retry with --as instead of being told only that it did not work.
func ambiguous(here []*session.Session) error {
	if len(here) == 0 {
		return fmt.Errorf("no active agent session in this room; pass --as <harness:session-id>")
	}
	keys := make([]string, 0, len(here))
	for _, s := range here {
		keys = append(keys, s.Key())
	}
	return fmt.Errorf("%d agents are active here and the caller could not be identified; retry with --as, one of: %s",
		len(here), strings.Join(keys, "  "))
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
