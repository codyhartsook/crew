# multiplayer

A shared context and live-session registry for coding agents in the same git
worktree.

Claude Code and Codex record their lifecycle through hooks. Agents then share
a durable room for decisions, findings, questions, and handoffs—none of it is
committed to the repository.

## Install

### From a release

```
curl -fsSL https://raw.githubusercontent.com/codyhartsook/multiplayer/main/scripts/install.sh | sh
```

The installer downloads the latest GitHub Release, verifies its checksum, and
installs the matching macOS/Linux amd64/arm64 binary in `~/.local/bin`. To pin
a release: `curl -fsSL https://raw.githubusercontent.com/codyhartsook/multiplayer/main/scripts/install.sh | MULTIPLAYER_VERSION=v0.1.0 sh`.

### From source

Requires Go 1.26.5 and `make`.

```sh
git clone https://github.com/codyhartsook/multiplayer.git
cd multiplayer
make install
```

This installs to `~/.local/bin`. For a system-wide installation:

```sh
sudo make install PREFIX=/usr/local
```

## Start

```sh
multiplayer init
```

Safe to run again. It configures the agent CLIs and starts the local broker;
use `multiplayer dashboard` in another terminal to open the UI.

## Commands

| Command | What it does |
| --- | --- |
| `init` / `uninstall` | Set up or remove the automatic integration. |
| `ls` | List active agent sessions. |
| `dashboard` (`fleet`) | Open the dashboard for the running local broker. |
| `post` / `resolve` / `remove` | Post an entry, answer one, or remove one of your unthreaded entries. |
| `room` | Show the room; `--inbox --ack` reads new addressed entries. |
| `search` | Find runbooks, state and entries by topic. |
| `promote` | Move an entry or state key up to the repository room. |
| `state` | What is currently true here, including runbooks. |

`multiplayer <command> --help` shows flags. `multiplayer uninstall --yes`
removes the integration and keeps your data.

## Design

Hooks update a local SQLite registry. The dashboard reads it; worktree rooms
hold shared context. Hooks never fail an agent session and log errors to
`~/.multiplayer/hook.log`.
