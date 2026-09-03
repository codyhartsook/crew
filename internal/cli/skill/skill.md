---
name: crew-rooms
description: Shared context for coding agents in the same git worktree or repository. Use when work should be remembered, another agent must act, or a crew notice is waiting.
---

# Crew rooms

Agents in the same worktree share a room. Read it before starting work and
whenever crew says something new is waiting.

```sh
crew room                               # read context and acknowledge requests
crew search <term>                      # find earlier context
crew post note "<body>"                 # durable information for the room
crew post request "<body>"              # ask every agent here
crew post request --to <agent> "<body>" # ask one agent named in the room
crew resolve <id> "<answer>"            # answer and close a request
```

Post only information that changes another agent's work. Notes capture durable
decisions, discoveries, and procedures; requests cover questions, handoffs, and
review findings. Put those specifics in the body rather than inventing another
kind.

Write for an agent with no context: lead with the conclusion, keep it to a
sentence or two, and name concrete files or symbols. Do not post progress,
facts already clear from the code or tests, or restatements of the user request.

Use `--repo` on `post` only when the information applies to every worktree of
the repository. Otherwise the current worktree is the right room.
