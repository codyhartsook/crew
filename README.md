# multiplayer

A registry of running coding-agent sessions, and a shared context for the agents
working in each git worktree.

Claude Code and Codex call `multiplayer hook` from their session lifecycle
hooks. Each call works out where it is - the git checkout, and the pooled
worktree slot if it is one - and records the session. Agents in the same worktree
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
| `install` / `uninstall` | Set up or remove the automatic integration. |
| `ls` | List active agent sessions. |
| `fleet` | Open the dashboard. |
| `post` / `resolve` / `remove` | Post an entry, answer one, or remove one of your unthreaded entries. |
| `room` | Show the room; `--inbox --ack` reads new addressed entries. |
| `search` | Find runbooks, state and entries by topic. |
| `promote` | Move an entry or state key up to the repository room. |
| `state` | What is currently true here, including runbooks. |

The hook, server and pruning commands are automation plumbing; installation and
`fleet` handle them for normal use.

`multiplayer <command> --help` for flags.

## Design

```
harness hook ──► internal/hook ──► store.Store ──┬─► sqlitestore  (default)
                      │                          └─► httpstore ──► internal/api
        ┌─────────────┴─────────────┐                                    ▲
internal/detect            internal/harness                internal/ui  (dashboard)
(git + worktree pools)     (one row per agent CLI)
```

Three seams keep the variable parts out of the core:

- `store.Store` hides how records are persisted. Nothing outside
  `internal/store/...` knows, and `internal/store/storetest` is a conformance
  suite every implementation passes.
- `detect.Provider` recognizes a checkout lent out by a worktree pool manager.
- `harness.Spec` is one table row per coding-agent CLI: its config paths, env
  markers, process name and hook timeouts.

Supporting another store, pool manager or harness is one implementation or one
row, not an edit in every package that cares.

The hook cannot break an agent. It exits 0 whatever happens, logging to
`~/.multiplayer/hook.log`.

Rationale for the smaller decisions lives in comments beside the code.
