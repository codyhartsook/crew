# Agent guide

## Build and test

Needs Go 1.26.5 and make.

```sh
make build     # build/crew
make install   # install to GOBIN, or GOPATH/bin
make check     # gofmt, go vet, go test
```

`crew init` refuses a `go run` binary, so use `make install` first. Releases
are described in `docs/releasing.md`.

## Layout

| Path | Holds |
| --- | --- |
| `cmd/crew` | The entry point. |
| `internal/cli` | The commands, mostly one package each, plus the broker and dashboard in `server`. |
| `internal/store` | The persistence boundary. SQLite is the implementation. |
| `internal/harness` | What is specific to Claude Code and Codex. |
| `internal/hook`, `internal/notify` | Lifecycle records and delivery into live sessions. |
| `internal/room`, `internal/documents` | Shared context and the document store. |

`docs/architecture.md` explains the channels and the role skill lifecycle.

## Design

Hooks update a local SQLite registry. Addressed posts signal the broker
immediately, with a one-second poll as fallback. The dashboard reads the same
store. Hooks never fail an agent session. They log errors to
`~/.multiplayer/hook.log`, and the broker logs to `~/.multiplayer/broker.log`.

## Commands

| Command | What it does |
| --- | --- |
| `init` / `uninstall` | Set up or remove the integration. |
| `ls` | List active agents and their roles. |
| `role` | Take the role the user gave you, or `--drop` it. |
| `anchor` | Mark a plain folder as a room root. |
| `dashboard` | Open the dashboard. |
| `post` / `resolve` | Post an entry, or answer and close a request. |
| `remove` | Delete one of your own entries. |
| `room` | Show the room, or the newest `--last <n>`. |
| `search` | Find earlier entries by topic. |
| `docs` / `docs ls` | List the room's documents. Also `publish`, `unpublish`, `open`. |

Run `crew <command> --help` for flags.

## Usage

* Address a request: `crew post request --to moss-otter "Can you check this?"`. Names come from `crew ls`.
* Give a role: when the user says "you're the tester", run `crew role tester "<description>"`. Agents in that room send matching work to you until the session ends.
* Share a plain folder: run `crew anchor ~/work/notes` once. Every agent started underneath shares one room. Without an anchor, each directory is its own room.
* Share a document: write a file under the path from `crew docs --path`, then run `crew docs publish <file>`. Add `--repo` to publish for every worktree.
