# multiplayer

A registry of running coding-agent sessions, and a shared context for the agents
in each git worktree.

Claude Code and Codex call `multiplayer hook` from their session lifecycle
hooks. Each call works out where it is - the git checkout, and the treehouse
pool slot if it is one - and records the session.

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

Builds to `~/.local/bin/multiplayer` and registers with both harnesses. The
store is created and migrated on first use. Upgrade by running it again.

| | Claude Code | Codex |
| --- | --- | --- |
| Hooks | `~/.claude/settings.json` | `~/.codex/hooks.json` |
| Skill | `~/.claude/skills/multiplayer-rooms/` | `~/.codex/skills/multiplayer-rooms/` |
| Sandbox grant | not needed | `~/.codex/config.toml` |

Everything is backed up before it is written. Reinstalling replaces only the
entries this tool owns, and a skill you have edited is left alone with the new
version dropped at `SKILL.md.new`. `multiplayer uninstall --yes` removes it all
and keeps your data; `--purge` takes the store too.

Codex asks you to trust a newly added hook the first time it runs. Do not
install from `go run` - the hooks would record a build path that stops existing,
and `install` refuses when it notices.

## Rooms

Agents in the same worktree share a **room**. Nothing in it is committed to the
repository: it is process, not product.

```
multiplayer post decision "sqlite over postgres; the Store interface keeps it swappable"
multiplayer post question "who owns the retry policy?"
multiplayer inbox --ack
multiplayer resolve 3 "the gateway does"
multiplayer search cluster
```

`decision` and `finding` are reference material, shown in the briefing an agent
gets on arrival. `question`, `handoff` and `review` are addressed to somebody
and stay open until resolved; a one-line notice appears at the start of a turn
while something is waiting.

Sessions join their worktree room and the wider repository room automatically
(`MULTIPLAYER_AUTO_JOIN=false` to opt out).

### State and runbooks

Entries record what happened; **state records what is currently true**, and is
replaced in place.

```
multiplayer state set migration/status "tables done, indexes pending"
multiplayer state set procedure/cluster-update "$(cat runbook.txt)"
multiplayer state get procedure/cluster-update
```

Keys namespace by prefix. A briefing names long values and runbooks rather than
reproducing them, so a session learns they exist without paying for the body.

### Reviews

A review is a batch of findings, each an ordinary entry, so each resolves on its
own and carries why.

```
multiplayer review start "verbosity in the store layer"
multiplayer review add r1 --severity must --file internal/cli/install.go --line 23 "..."
multiplayer review show r1
multiplayer resolve 3 "declined: that sentence is the Upsert contract"
```

Markdown is the output, rows are the storage. A report cannot hold what happened
to each finding, and *declined, because X* is what stops the next reviewer
proposing the same change.

## Dashboard

```
multiplayer fleet
```

Read-only, grouped by worktree or repository, refreshing every two seconds. Two
live agents in one worktree are flagged. One self-contained HTML file compiled
into the binary, so it works with no network.

## Commands

| Command | What it does |
| --- | --- |
| `hook` | Record a lifecycle event from stdin. Called by the harnesses. |
| `ls` | List live sessions. `--all`, `--harness`, `--repo`, `--treehouse`, `--json`. |
| `get` / `rm` | Show or delete session records. |
| `fleet` / `serve` | Open the dashboard, or serve the API and dashboard. |
| `post` / `resolve` | Post an entry, or answer and close one. |
| `room` / `inbox` | The room briefing, or what is addressed to you. |
| `search` | Find runbooks, state and entries by topic. |
| `state` | `set`/`get`/`ls`/`rm` what is currently true here. |
| `review` | `start`/`add`/`show`/`list` code reviews. |
| `join` / `leave` / `clear` | Room membership, and deleting a room's entries. |
| `prune` | End sessions whose agent process is gone. |
| `install` / `uninstall` / `version` | Registration and diagnostics. |

## Storage

SQLite at `~/.multiplayer/sessions.db`, overridable with `--db` or
`$MULTIPLAYER_DB`. Setting `--server` or `$MULTIPLAYER_SERVER` points commands
at a running `multiplayer serve` instead, which owns the database on their
behalf. Room commands need the local database; the HTTP store carries sessions
only.

`serve` listens on loopback and has no authentication of its own.

| Method | Path |
| --- | --- |
| `GET` | `/healthz` |
| `GET` `POST` | `/v1/sessions` |
| `GET` `DELETE` | `/v1/sessions/{key}` |
| `POST` | `/v1/sessions/{key}/end`, `/v1/sessions/{key}/touch` |
| `GET` | `/v1/entries`, `/v1/state` |

## Design

```
harness hook ──► internal/hook ──► store.Store ──┬─► sqlitestore  (default)
                      │                          └─► httpstore ──► internal/api
                      ▼                                                  ▲
               internal/detect  (git + treehouse)              internal/ui  (dashboard)
```

**`store.Store` is the seam.** Nothing outside `internal/store/...` knows how
records are persisted, and `internal/store/storetest` is a conformance suite
every implementation passes - `sqlitestore` directly, the HTTP API through
`httpstore` - so the server is interchangeable with the local database rather
than merely similar to it.

**The hook cannot break an agent.** It exits 0 whatever happens, logging to
`~/.multiplayer/hook.log`. Detection is best effort, git calls are capped, and a
session outside a checkout is still recorded.

**A room has two halves on purpose.** Entries are append-only, which makes
concurrent writes safe and resolution a new row rather than an edit. State is
keyed and overwritten, because a current value is not an event.

Rationale for the smaller decisions lives in comments next to the code that
implements them.
