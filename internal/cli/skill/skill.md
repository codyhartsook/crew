---
name: crew-rooms
description: Coordinate coding agents through shared rooms and isolated delegated roles. Use when work should be remembered, another agent can take a bounded task, a change needs adversarial review, or a crew notice is waiting.
---

# Crew coordination

Room agents share context. Delegated roles do not join the room. If this is a
delegated session (`CREW_DELEGATION` is set), skip to
[Delegated roles](#delegated-roles).

## Rooms

Read the room when the session-start notice says it holds entries and whenever
crew says new work is waiting.

```sh
crew whoami
crew ls
crew room
crew search <term>
crew post note "<body>"
crew post request "<body>"
crew post request --to <agent> "<body>"
crew resolve <id> "<answer>"
crew docs --path
crew publish <file>
```

Post only information that changes another agent's work. Notes record durable
decisions, findings, procedures, handoffs, or reviews. Requests ask for action
or an answer and remain open until resolved.

Write for an agent with no context: lead with the conclusion, keep it to a
sentence or two, and name concrete files or symbols. Do not post progress,
facts already clear from code or tests, or restatements of the user request.

Use `crew whoami` for your name and `crew ls` for other room agents. For a
named recipient, always use `--to`; do not put the alias only in the body or
duplicate the request elsewhere. Close every request with `crew resolve`, even
when you can only redirect it or explain why you cannot help.

Search before anything multi-step or unfamiliar when another agent may have
already recorded the procedure or why the obvious approach fails.

## Delegation

```sh
crew roles
crew delegate <role> "<task>"
crew delegate result <id>
crew answer <id> "<body>"
```

Delegate bounded work that can return a focused result. Give the role its
scope, constraints, and expected result. Use a room request when an existing
room agent needs shared context; keep tightly coupled implementation and
integration decisions here.

Prefer asynchronous delegation. With `--wait`, the requester is blocked and
cannot answer the role's `crew ask`. Do not create both a delegation and a room
request for the same task.

`crew answer` answers a delegated role; `crew resolve` closes a room request.

## Delegated roles

Work only on the delegated task. Do not use room, search, post, or resolve
commands. Ask only when missing context would materially change the result:

```sh
crew ask "<question>"
crew memory [body]
```

If an ask times out, finish with available evidence and name what is unknown.
Memory is for reusable private guidance; return findings as the task result.

## Scope and documents

Room commands target the current worktree. Use `--repo` for information that
must outlive it; it does not apply to an anchored folder. Agents may access
only rooms they joined.

For a long or revisable artifact, write under `crew docs --path`, then publish
it. Publishing announces the file without copying it into agent context;
revise it in the store because publishing the same name twice fails.

## Adversarial review

Ask another room agent to attack a change before it ships. A sibling worktree
cannot see uncommitted files, so commit and name the ref. Start the request
with `review:` and say what you are unsure about.

```sh
crew post request --to <agent> "review: HEAD~2..HEAD, does the retry path double-apply on partial failure?"
```

As reviewer, try to disprove the change rather than confirm it. Prioritize
failures that are expensive or hard to detect. Each finding names the file and
line, consequence, and fix; one defensible finding beats five weak ones.

Review only: do not fix what you find, edit the requester's tree, or review your own change.
Lead the resolution with `approve` or `needs attention`; put longer results in
a document.
