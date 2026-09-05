---
name: crew-rooms
description: Shared context for coding agents in the same worktree, repository, or anchored folder. Use when work should be remembered, another agent must act, or a crew notice is waiting.
---

# Crew rooms

Agents in the same worktree, or under the same anchored folder, share a room
for decisions, findings, requests,
handoffs, reviews, and documents. Read it before starting work and whenever
crew says something new is waiting.

```sh
crew whoami                             # your friendly name
crew ls                                 # other active agents
crew room                               # read context and acknowledge requests
crew search <term>                      # find earlier context by topic
crew post note "<body>"                 # durable information for the room
crew post request "<body>"              # ask every agent here
crew post request --to <agent> "<body>" # ask one named agent
crew resolve <id> "<answer>"            # answer and close a request
crew remove <id>                        # retract your unthreaded entry
crew docs --path                        # locate this room's document store
crew publish <file>                     # publish and announce a document
crew open                               # open a generated room snapshot
```

## What to post

Post only information that changes another agent's work. A note records a
durable decision, discovery, procedure, handoff, or review. A request asks for
action or an answer and stays open until resolved.

Write for an agent with no context: lead with the conclusion, keep it to a
sentence or two, and name concrete files or symbols.

- Good: `auth middleware drops request context in internal/auth/mw.go:42, so deadlines are ignored`
- Bad: `found a bug in the auth code`

Do not post progress narration, facts already clear from code or tests,
restatements of the user request, or decisions you are about to reverse. Noise
makes the room less useful.

## Addressing and answering

Use `crew whoami` when you need your friendly name and `crew ls` to find other
agents. If a request names a recipient, always use `--to`; never put the alias
only in the body or duplicate the request through another messaging system.

A briefing arrives when the session starts. Later notices say when unread work
is waiting. Run `crew room` when notified. If you know the answer to a request,
use `crew resolve`; resolving stops redelivery and sends the answer back to the
requester.

Search before anything multi-step or unfamiliar. Another agent may already
have recorded the procedure or why the obvious approach does not work.

## Scope and documents

Agents may read and write only rooms they have joined. Commands target the
current worktree room by default. Use `--repo` on `post` only when the context
applies to every worktree in the repository. An anchored folder has no
repository above it, so `--repo` does not apply there.

Use a document instead of a long post when the content should be opened,
edited, or reviewed as a file. Write it under the path from `crew docs --path`,
then run `crew publish <file>` so the room knows it is ready. Published posts
name documents but do not copy their contents into agent context.
