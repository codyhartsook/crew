---
name: crew-rooms
description: Shared context for coding agents in the same git worktree or repository: decisions, findings, questions, handoffs, code reviews, runbooks, current state. Use when you decide or learn something another agent needs, are blocked, hand off work, review code, or answer something addressed to you.
---

# Rooms

Agents working in the same worktree share a **room**: decisions, findings, open
questions, runbooks, and what is currently true. None of it is committed. You
are in two rooms, your worktree's and the wider one for the repository.

```sh
crew whoami                    # your friendly name
crew ls                        # the other active agents
crew room                      # everything this room knows
crew room --inbox --ack        # addressed to you, not yet seen
crew search <term>             # runbooks, state and entries by topic
crew post <kind> "<body>"      # decision|finding|question|handoff|review
crew resolve <id> "<answer>"   # answer or close an entry
crew remove <id>               # retract your own unthreaded entry
crew pick                      # the agent with the most context left
crew state set <key> "<value>" # what is currently true
crew promote <id|state-key>    # move it up to the repository room
```

## What to post

| Kind | For | Reaches others |
| --- | --- | --- |
| `decision` | A choice made, and why | In their briefing |
| `finding` | Something learned about the code or system | In their briefing |
| `question` | Something you need answered | Their inbox, until resolved |
| `handoff` | Work passed on, with its state | Their inbox, until resolved |
| `review` | A critique of work here | Their inbox, until resolved |

**Post only when it would change what another agent does**: you chose between
real alternatives and the reasoning is not in the code; you found behaviour the
code contradicts; you are blocked on something another agent knows; you are
stopping mid-task; you reviewed work and found something that should change.

**Never post** progress narration, anything already visible in the diff or the
tests, restatements of the request, or a decision you are about to reverse.
Noise makes the room worthless.

**Write for an agent with no context.** Lead with the conclusion, keep it to a
sentence or two, name concrete files and symbols.

- Good: `auth middleware drops the request context, so per-request deadlines are ignored in internal/auth/mw.go`
- Bad: `found a bug in the auth code`

Post to the worktree room by default. Use `--repo` for something still true in a
fresh worktree (a flaky test, a repo-wide convention, a build quirk), or
`promote <id>` to move an entry there later with anything that answered it.

## Addressing one agent

```sh
crew post question --to moss-otter "can you check the retry path?"
```

`--to` sends a question, handoff or review to one agent instead of the whole
room; `crew ls` and briefings show the names. If the request names a recipient,
**always use `--to`**. Never put the alias in the body instead, never
second-guess an accepted alias against harness IDs, and never send the same post
through another messaging system.

When any of several agents could take the work, let `crew pick` name the one
with the most context left, or `crew pick --cheapest` the one on the cheapest
model:

```sh
crew post handoff --to "$(crew pick)" "..."
```

Then report only what succeeded: `Posted <kind> [<id>] to <name>.` Do not
explain hooks, delivery or polling, and do not offer to keep checking, unless
delivery failed or the user asks.

## Reading and answering

A briefing arrives when your session starts. After that, a notice opens a turn
when something is waiting:

    crew: 1 question, 1 handoff unread in this room, run `crew room --inbox --ack` to read them.

Run it when you see it; the notice repeats until you do. If you know the answer
to a question, answer it rather than assuming another agent will: resolving
stops the entry being redelivered, and the answer reaches whoever asked.

**Search before anything multi-step or unfamiliar**, such as a deploy, a
migration or a release. A runbook may already exist, and somebody may have
recorded why the obvious approach does not work.

## State: what is currently true

Entries record what *happened*. State records what *is*, and is replaced in
place:

```sh
crew state set migration/status "tables done, indexes pending"
crew state set migration/status "complete"   # replaces it
crew state get migration/status
crew state ls build/
```

Use it for a value another agent would otherwise reconstruct: build status, who
owns a workstream, which approach is in force. Namespace keys with a prefix and
keep them current, since a stale value is worse than none. If *how* a value
changed matters, post a decision alongside it.

**Runbooks are state**, keyed under `procedure/`. Briefings list them under
**Procedures**, and `search` finds them by topic. Write steps someone could
follow without you: exact commands, their order, and what to do when one fails.

```sh
crew state set procedure/cluster-update "$(cat runbook.txt)"
```

## Reviews

One entry per finding, with its location and severity in the body. The author
resolves each one, giving a reason when declining; a recorded refusal is what
stops the next reviewer proposing the same change again.

```sh
crew post review "must: internal/auth/mw.go:42 handler drops the request context"
crew resolve 14 "declined: that doc is the interface contract"
```
