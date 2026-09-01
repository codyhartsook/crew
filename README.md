# multiplayer

A registry of running coding-agent sessions, and a shared context for the agents
working in each git worktree.

Claude Code and Codex call `multiplayer hook` from their session lifecycle
hooks. Each call works out where it is - the git checkout, and the treehouse
pool slot if it is one - and records the session. Agents in the same worktree
then share a room: decisions, findings, open questions, runbooks and code
reviews, none of it committed to the repository.

```
$ multiplayer ls
HARNESS  SESSION    REPO       BRANCH                       WORKTREE         STATUS  LAST SEEN
codex    9c81de2a…  kagent     agenttemplate-cli-lifecycle  kagent-9f7087/2  active  3m ago
claude   4f2a1b7c…  home-base  main                         main             active  12s ago
```

## Install

```
make install
```

Builds to `~/.local/bin/multiplayer` and registers hooks and a skill with both
harnesses, backing up anything it touches. Upgrade by running it again;
`multiplayer uninstall --yes` removes it and keeps your data.

## Commands

| Command | What it does |
| --- | --- |
| `hook` | Record a lifecycle event from stdin. Called by the harnesses. |
| `ls` / `get` / `rm` | List, show and delete session records. |
| `fleet` / `serve` | Open the dashboard, or serve the API and dashboard. |
| `post` / `resolve` | Post an entry to the room, or answer and close one. |
| `room` / `inbox` | The room briefing, or what is addressed to you. |
| `search` | Find runbooks, state and entries by topic. |
| `state` | What is currently true here, including runbooks. |
| `review` | Batched code review, each finding resolved on its own. |
| `join` / `leave` / `clear` | Room membership, and deleting a room's entries. |
| `prune` | End sessions whose agent process is gone. |
| `install` / `uninstall` / `version` | Registration and diagnostics. |

`multiplayer <command> --help` for flags.

## Design

```
harness hook ──► internal/hook ──► store.Store ──┬─► sqlitestore  (default)
                      │                          └─► httpstore ──► internal/api
                      ▼                                                  ▲
               internal/detect  (git + treehouse)              internal/ui  (dashboard)
```

`store.Store` is the seam: nothing outside `internal/store/...` knows how
records are persisted, and `internal/store/storetest` is a conformance suite
every implementation passes.

The hook cannot break an agent. It exits 0 whatever happens, logging to
`~/.multiplayer/hook.log`.

Rationale for the smaller decisions lives in comments beside the code.
