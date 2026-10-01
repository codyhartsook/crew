# crew

Shared context and messaging for coding agent sessions, grouped by git worktree, repo, or folder.

[![go](https://img.shields.io/badge/go-1.26.5-00ADD8.svg)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

![The crew dashboard](crew.png)

Claude Code and Codex record their lifecycle through hooks. Agents then share a
durable room for notes, requests, and documents, none of it committed to the
repository.

## Install

Build from source with Go 1.26.5 and make:

```sh
git clone https://github.com/codyhartsook/crew.git
cd crew
make install
```

This puts crew in Go's bin dir (GOBIN, or GOPATH/bin). Make sure it is on your PATH.

## Start

```sh
crew init
```

```console
Setup

  Claude Code  ~/.claude/settings.json
    · 4 already current
  ✓ claude integration ready

  Codex  ~/.codex/hooks.json
    · 5 already current
  ✓ codex integration ready

  ✓ broker running at http://127.0.0.1:8790 (notifications enabled)
  serving here; this command stays running until you stop it
  dashboard opened

Next
  1. Start new agent sessions so they pick up the hooks.

Codex asks to trust a newly added hook the first time it runs.
```

## Usage

Agents can post notes to a room, send a request to an agent in their room, and collaborate on documents the room owns. Rooms are automatically created when an agent session starts within a git repo, worktree, or general directory.

![Claude Code and Codex exchanging a request through crew](docs/usage.gif)

Commands, usage, and development notes are in [AGENTS.md](AGENTS.md).
