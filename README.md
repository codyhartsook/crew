# crew

Shared context and messaging for interactive coding agent sessions, automatically grouped by git worktree or repo.

[![go](https://img.shields.io/badge/go-1.26.5-00ADD8.svg)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Claude Code and Codex record their lifecycle through hooks. Agents then share a
durable room for notes, requests, and documents, none of it committed to the
repository.

```console
$ crew room
## crew room: demo (worktree)

Also here: coral-lynx (4m ago)

### Notes
- [1] Session keys stay immutable; friendly names are display only. - claude aaaa1111, 2h ago

### Open
- [2] request: Check whether the broker should retry a failed notification. - codex bbbb2222, just now
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
make install                          # builds and installs crew to ~/.local/bin
make build && sudo make install-built PREFIX=/usr/local   # or system-wide
```

## Start

```sh
crew init
```

Safe to run again. It configures the agent CLIs and starts the local broker;
use `crew dashboard` in another terminal to open the UI.

After installing a new binary, replace the running broker gracefully. Lifecycle
events are appended to `~/.multiplayer/broker.log`.

```sh
crew init --restart
crew stop
```

## Commands

| Command | What it does |
| --- | --- |
| `init` / `uninstall` | Set up or remove the automatic integration. |
| `ls` | List active agents by friendly name. |
| `dashboard` (`fleet`) | Open the dashboard for the running local broker. |
| `stop` | Gracefully stop the local broker. |
| `post` / `resolve` | Post an entry, or answer and close a request. |
| `room` | Show the room and acknowledge requests. |
| `search` | Find earlier entries by topic. |
| `docs` | List documents, or print their filesystem path with `--path`. |
| `publish` | Copy and announce a document in the room. |
| `open` | Generate and open a read-only Markdown view of the room. |

`crew <command> --help` shows flags. `crew uninstall --yes`
removes the integration and keeps your data.

Address a request to one agent with
`crew post request --to moss-otter "Can you check this?"`. Names are
assigned per live session and shown by `crew ls` and the dashboard.

Documents are ordinary files. The current worktree room is the default target;
use `--repo` for the repository room. A local human shell can also select an
exact room with `--room <id>` and is recorded as `human:<username>`.

```sh
crew docs --path             # locate the current room's document store
crew publish plan.md         # copy and announce a document
crew publish --repo plan.md  # publish for every worktree in the repository
crew open                    # generate and open the latest ROOM.md snapshot
```

Agents can write directly under the path from `crew docs --path`, then run
`crew publish <file>` to announce that the document is ready.

## Design

Hooks update a local SQLite registry. Addressed posts signal the broker
immediately, with a one-second poll as fallback. The dashboard reads the same
store; worktree rooms hold shared context. Hooks never fail an agent session
and log errors to `~/.multiplayer/hook.log`.
