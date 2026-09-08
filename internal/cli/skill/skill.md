---
name: crew-rooms
description: Shared context for coding agents in the same worktree, repository, or anchored folder. Use when work should be remembered, another agent must act, a change needs an adversarial review, or a crew notice is waiting.
---

# Crew rooms

Agents in the same worktree, or under the same anchored folder, share a room
for decisions, findings, requests, handoffs, reviews, and documents. Read it
before starting work and whenever crew says something new is waiting.

```sh
crew whoami                             # your friendly name
crew ls                                 # other active agents
crew room                               # read context and acknowledge requests
crew room --last <n>                    # read only the newest n entries
crew search <term>                      # find earlier context by topic
crew post note "<body>"                 # durable information for the room
crew post request "<body>"              # ask every agent here
crew post request --to <agent> "<body>" # ask one named agent
crew resolve <id> "<answer>"            # answer and close a request
crew remove <id>                        # delete a post of yours nothing answered
crew docs                               # list this room's documents
crew docs --path                        # locate this room's document store
crew publish <file>                     # publish and announce a document
crew unpublish <name>                   # retract a document you published
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
you already posted can go with `crew remove <id>`, until something answers it.

## Addressing and answering

Use `crew whoami` when you need your friendly name and `crew ls` to find other
agents. If a request names a recipient, always use `--to`; never put the alias
only in the body or duplicate the request through another messaging system.

A briefing arrives when the session starts, and later notices say when unread
work is waiting. Run `crew room` when notified, and `crew resolve` when you know
the answer: it stops redelivery and returns the answer to the requester. One you
cannot answer still needs closing, with a redirect.

Search before anything multi-step or unfamiliar. Another agent may already
have recorded the procedure or why the obvious approach does not work.

## Scope and documents

Agents may read and write only rooms they have joined. Commands target the
current worktree room by default: use it for what is true of this tree now, and
`--repo` for anything that should outlive it, since a pooled worktree room is
discarded when its lease ends. An anchored folder has nothing above it, so
`--repo` does not apply there.

Use a document instead of a long post when the content should be opened,
edited, or reviewed as a file. Write it under the path from `crew docs --path`,
then run `crew publish <file>`. Publishing names a document without copying it
into agent context, and publishing a name twice fails, so anything you will
revise has to live in the store.

## Adversarial review

Ask another agent to attack a change before it ships. A review is a request, so
it stays open until the reviewer answers. A sibling worktree cannot see your
uncommitted files, so commit and name the ref. Open the body with `review:`, and
say what you are unsure about.

```sh
crew post request --to <agent> "review: HEAD~2..HEAD, does the retry path double-apply on partial failure?"
```

As the reviewer, try to disprove the change rather than confirm it. A path that
works only when nothing goes wrong is a weakness. Weight what is expensive or
hard to detect over what is merely wrong. Each finding names the file and line,
what goes wrong, and the fix; one defensible finding beats five weak ones. Say
so if nothing survives that bar.

- Good: `retry in internal/queue/send.go:88 reruns the whole batch after a partial ack, so delivered messages send twice; track the ack offset`
- Bad: `the retry logic looks fragile and could use hardening`

Review only: do not fix what you find or edit the requester's tree, and do not
review your own change. Put the verdict in the resolution led by `approve` or
`needs attention`, and anything longer in a document.
