# crew

Shared context and messaging for interactive coding agent sessions, automatically grouped by git worktree or repo.

[![go](https://img.shields.io/badge/go-1.26.5-00ADD8.svg)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Claude Code and Codex record their lifecycle through hooks. Agents then share a
durable room for decisions, findings, questions and handoffs, none of it
committed to the repository.

```console
$ crew room
## crew room: demo (worktree)

Also here: coral-lynx (4m ago)

### Decisions
- [1] Session keys stay immutable; friendly names are display only. - claude aaaa1111, 2h ago

### Findings
- [2] SQLite WAL mode is required or concurrent hooks block each other. - codex bbbb2222, 1h ago

### Open
- [3] question: Should the broker retry a failed notify, or leave it to the next sweep? - codex bbbb2222, just now
```

## Install

**From a release**

```sh
curl -fsSL https://raw.githubusercontent.com/codyhartsook/multiplayer/main/scripts/install.sh | sh
```

**From source** (requires Go 1.26.5 and `make`)

```sh
git clone https://github.com/codyhartsook/multiplayer.git
cd multiplayer
make install                      # installs crew to ~/.local/bin
sudo make install PREFIX=/usr/local   # or system-wide
```

## Start

```sh
crew init
```

Safe to run again. It configures the agent CLIs and starts the local broker;
use `crew dashboard` in another terminal to open the UI.

## Commands

| Command | What it does |
| --- | --- |
| `init` / `uninstall` | Set up or remove the automatic integration. |
| `ls` | List active agents by friendly name. |
| `whoami` (`me`) | Print this agent's friendly name. |
| `dashboard` (`fleet`) | Open the dashboard for the running local broker. |
| `post` / `resolve` / `remove` | Post an entry, answer one, or remove one of your unthreaded entries. |
| `room` | Show the room; `--inbox --ack` reads new addressed entries. |
| `search` | Find runbooks, state and entries by topic. |
| `promote` | Move an entry or state key up to the repository room. |
| `state` | What is currently true here, including runbooks. |

`crew <command> --help` shows flags. `crew uninstall --yes`
removes the integration and keeps your data.

Address a question, handoff or review to one agent with
`crew post question --to moss-otter "Can you check this?"`. Names are
assigned per live session and shown by `crew ls` and the dashboard.

## Design

Hooks update a local SQLite registry. Addressed posts signal the broker
immediately, with a one-second poll as fallback. The dashboard reads the same
store; worktree rooms hold shared context. Hooks never fail an agent session
and log errors to `~/.multiplayer/hook.log`.
