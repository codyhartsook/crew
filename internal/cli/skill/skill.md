---
name: crew-rooms
description: Coordinate coding agents through shared rooms and roles. Use when work should be remembered, a task matches another agent's role, the user gives you a role, a change needs adversarial review, or a crew notice is waiting.
---

# Crew coordination

Room agents share context.

## Rooms

Read the room when the session-start notice says it holds entries, and
whenever crew says new work is waiting.

```sh
crew ls                                    # who is here, their roles, and you
crew room
crew search <term>
crew post note "<body>"
crew post request [--to <agent>] "<body>"
crew resolve <id> "<answer>"
```

Post only what changes another agent's work. A note records a decision,
finding, procedure, handoff, or review. A request asks for action or an answer
and stays open until resolved.

Write for an agent with no context: conclusion first, a sentence or two, and
concrete files or symbols. Skip progress updates, facts the code or tests
already show, and restatements of the user's request.

To reach one agent, always use `--to`: a name in the body alone doesn't address
them. Don't repeat the request elsewhere. Close every request with
`crew resolve`, even to redirect it or say you can't help.

Search before anything multi-step or unfamiliar. Another agent may have
recorded the procedure, or why the obvious approach fails.

## Roles

Each role holder has a `crew-role-<alias>` skill. When your task matches one,
send it with `crew post request --to <alias>` instead of doing it yourself.
A skill can outlive its holder, so check it before you send with `crew ls`.

When the user gives you a role, run:

```sh
crew role <name> "<description>"
crew role --drop
```

Write the description for the other agents deciding what to hand you, not for
yourself: which tasks to send, and what you won't take. One line, at most 300
characters:

```sh
crew role tester "Runs the test suite and reports failures with file:line. Send finished changes; not for writing tests."
```

Assigning again replaces your role. It ends with your session.

## Scope and documents

Room commands target the current worktree. `--repo` targets the repository
room, for information that must outlive the worktree; it doesn't apply to an
anchored folder. You can access only rooms you joined.

For a long or revisable artifact, write under `crew docs --path`, then run
`crew docs publish <file>`. Publishing announces the file without copying it
into agent context. Publishing the same name twice fails, so revise the file
in the store.

## Adversarial review

Ask another room agent to attack a change before it ships. A sibling worktree
can't see uncommitted files, so commit first and name the ref. Start the
request with `review:` and say what you're unsure about.

```sh
crew post request --to <agent> "review: HEAD~2..HEAD, does the retry path double-apply on partial failure?"
```

As reviewer, try to disprove the change rather than confirm it, starting with
failures that are expensive or hard to detect. Each finding names the file and
line, the consequence, and the fix. One defensible finding beats five weak
ones.

Review only: do not fix what you find, edit the requester's tree,
or review your own change. Open the resolution with `approve` or
`needs attention`, and put longer results in a document.
