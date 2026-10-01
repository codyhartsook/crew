package roomctx

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/proc"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// sessionKeysFromEnv lists every session id the environment advertises. It survives
// a sandbox, but a nested harness inherits variables, so the first may not be ours.
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

// AgentEnvironment reports whether a harness identified the current process, so
// human commands do not attribute a person's shell to the lone active agent.
func AgentEnvironment() bool { return len(sessionKeysFromEnv()) > 0 }

// ResolveAuthor works out which registered session is running this command,
// trying the cheapest and most certain source first.
func ResolveAuthor(ctx context.Context, st store.Store, roomKeys []string) (string, error) {
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
		if inRooms(s, roomKeys) {
			here = append(here, s)
		}
	}
	inRoom := map[string]bool{}
	for _, s := range here {
		inRoom[s.Key()] = true
	}

	// 1. The harness told us who we are. Among several (nested harness), keep
	//    registered ones, then the one in this room.
	if matches := filterKnown(sessionKeysFromEnv(), known); len(matches) > 0 {
		if narrowed := filterHere(matches, here); len(narrowed) == 1 {
			return narrowed[0], nil
		}
		if len(filterHere(matches, here)) == 0 {
			return "", fmt.Errorf("the active agent is not in this room")
		}
	}
	// 2. An ancestor process is a registered harness; unavailable in a sandbox.
	//    Skip a shared or out-of-room pid: a wrong guess files under another agent.
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

// ResolveAgent turns an agent's friendly name into the session key that owns it.
// Names are unique among active sessions, so an ambiguous one means registry drift.
func ResolveAgent(ctx context.Context, st store.Store, roomKeys []string, name string) (string, error) {
	active, err := st.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return "", err
	}
	var matches []string
	for _, s := range active {
		if s.Alias == name && inRooms(s, roomKeys) {
			matches = append(matches, s.Key())
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%s isn't active in this room: it may have left, so any role it held is free; check crew ls", name)
	}
	return "", fmt.Errorf("agent name %q is ambiguous", name)
}

// Self is the calling agent's session key, or "" in a person's shell.
func Self(ctx context.Context, st store.Store, cwd string) string {
	if !AgentEnvironment() {
		return ""
	}
	key, err := ResolveAuthor(ctx, st, room.Keys(room.For(placeFor(ctx, cwd))))
	if err != nil {
		return ""
	}
	return key
}

// OwnAlias is the friendly name of the session running this command.
func OwnAlias(ctx context.Context, st store.Store, roomKeys []string) (string, error) {
	key, err := ResolveAuthor(ctx, st, roomKeys)
	if err != nil {
		return "", err
	}
	sess, err := st.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return room.Display(sess), nil
}
