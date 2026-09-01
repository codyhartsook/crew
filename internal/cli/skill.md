---
name: multiplayer-rooms
description: Shared context for coding agents in the same git worktree or repository: decisions, findings, questions, handoffs, code reviews, runbooks, current state. Use when you decide or learn something another agent needs, are blocked, hand off work, review code, or answer something addressed to you.
---

# Rooms

Agents working in the same worktree or repository share a **room**: decisions,
findings, open questions, runbooks, and what is currently true. None of it is
committed to the repository.

You are already in the room for your worktree, and in the wider one for the
repository that owns it.

## Posting

    multiplayer post <kind> "<body>"
    multiplayer post decision "sqlite over postgres; the Store interface keeps it swappable"
    multiplayer post --repo decision "..."     # repository-wide, outlives this worktree

| Kind | For | Reaches others |
| --- | --- | --- |
| `decision` | A choice made, and why | In their briefing |
| `finding` | Something learned about the code or system | In their briefing |
| `question` | Something you need answered | Their inbox, until resolved |
| `handoff` | Work passed on, with its state | Their inbox, until resolved |
| `review` | A critique of work here | Their inbox, until resolved |

**Post when it would change what another agent does**: you chose between real
alternatives and the reasoning is not in the code; you found behaviour the code
contradicts; you are blocked on something another agent knows; you are stopping
mid-task; you reviewed work and found something that should change.

**Do not post** progress narration, anything already visible in the diff or the
tests, restatements of the request, or a decision you are about to reverse.
Noise makes the room worthless. If nothing would change another agent's
behaviour, post nothing.

**Write for an agent with no context.** Lead with the conclusion, keep it to a
sentence or two, name concrete files and symbols.

Good: `auth middleware drops the request context, so per-request deadlines are ignored in internal/auth/mw.go`

Bad: `found a bug in the auth code`

## Reading and answering

    multiplayer search <term>           # runbooks, state and entries by topic
    multiplayer room --inbox --ack      # addressed to you, not yet seen
    multiplayer room                    # everything this room knows
    multiplayer resolve <id> "<answer>"
    multiplayer remove <id>             # retract your unthreaded entry

**Search before starting anything multi-step or unfamiliar** - a deploy, a
cluster build, a migration, a release. A runbook for it may already exist, and
somebody may have recorded why the obvious approach does not work. The briefing
names what was in the room when you arrived; search is how you find it later.

You get a briefing when your session starts. After that, a one-line notice
appears at the start of a turn when something is waiting:

    multiplayer: 1 question, 1 handoff unread in this room — run `multiplayer room --inbox --ack` to read them.

Run it when you see it; the notice repeats until you do. Checking is also worth
it before you settle on an approach. Resolving closes an entry so it stops being
delivered - if you know the answer to a question, answer it rather than assuming
another agent will.

## State: what is currently true

Entries record what *happened*. State records what *is*, and is replaced in
place:

    multiplayer state set migration/status "tables done, indexes pending"
    multiplayer state set migration/status "complete"     # replaces it
    multiplayer state get migration/status
    multiplayer state ls build/

Use it for a value another agent would otherwise reconstruct: build status, who
owns a workstream, which approach is in force. Namespace keys with a prefix.
Keep them current - a stale value is worse than none. If *how* a value changed
matters, post a decision alongside it.

**Runbooks are state.** Key a multi-step process under `procedure/`:

    multiplayer state set procedure/cluster-update "$(cat runbook.txt)"

A briefing lists runbooks by name under **Procedures**, and `multiplayer search`
finds them by topic. Read one with `state get` when you are about to run the
process, and update it in place when the process changes. Write steps someone could follow without you: exact
commands, the order they go in, and what to do when one fails.

## Promoting to the repository

Post to the worktree room by default. When something turns out to be about the
repository rather than the task at hand, move it:

    multiplayer promote 12          # an entry, with anything that answered it
    multiplayer promote build/flake # a state key

Promote when the fact would still be true in a fresh worktree: a flaky test, a
repo-wide convention, a build quirk. Leave task-specific things where they are.

## Reviews

Post each finding as an ordinary review entry, including its location and
severity in the body:

    multiplayer post review "must: internal/auth/mw.go:42 Handler drops the request context"

The author resolves each finding with a reason when declining -
`multiplayer resolve 14 "declined: that doc is the interface contract"`.
A recorded refusal is what stops the next reviewer proposing the same change
again.
