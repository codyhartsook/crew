# multiplayer

A registry of running coding-agent sessions: which agents are alive, what
harness they are, and which git repository or worktree each one is working in.

Claude Code and Codex both call `multiplayer hook` from their session lifecycle
hooks. Each call works out where it is - the git checkout, and the treehouse
pool slot if the checkout is one - and records the session. At any moment you
can ask what is running and where.

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

That builds the binary to `~/.local/bin/multiplayer` and registers it. Nothing
else is needed - the store is created and migrated on first use.

`make install` is two steps you can also run apart, because putting a binary on
PATH and registering it with a harness are different jobs:

```
make build                 # go build -o ~/.local/bin/multiplayer
multiplayer install        # register hooks, skill, sandbox grant
```

What `install` touches, all of it backed up first:

| | Claude Code | Codex |
| --- | --- | --- |
| Hooks | `~/.claude/settings.json` | `~/.codex/hooks.json` |
| Skill | `~/.claude/skills/multiplayer-rooms/` | `~/.codex/skills/multiplayer-rooms/` |
| Sandbox grant | not needed | `~/.codex/config.toml` |

Three events per harness: `SessionStart`, `UserPromptSubmit`, `SessionEnd`.
`--claude` or `--codex` limits it to one; `--dry-run` reports without writing.

**Reinstalling is safe, and so is editing the skill.** Hook entries this tool
owns are replaced and others left alone. The skill is only overwritten when it
is byte-for-byte what this tool last wrote - a hash recorded beside it says so.
Edit it and a reinstall leaves your version in place, dropping the new one at
`SKILL.md.new`.

**Do not install from `go run`.** The hooks would record a build-cache path that
stops existing, and every hook would then fail silently. `install` refuses when
it notices, and tells you to build first.

Codex asks you to trust a newly added hook the first time it runs, and records
the answer in `~/.codex/config.toml`. Editing `hooks.json` later invalidates
that, so expect one more prompt after an upgrade that changes the hooks.

```
multiplayer version           # binary, schema version, store path
multiplayer uninstall         # lists what would go
multiplayer uninstall --yes   # removes it; --purge takes the store too
```

Uninstall removes only what install added. A skill you edited is kept, since
removing the tool is no reason to discard your tuning.

## Upgrading

`make install` again. The store migrates itself forward on open. Going the other
way is refused: a database a newer binary has migrated will not open under an
older one, with an error saying to upgrade rather than a subtle misread.

## Dashboard

```
multiplayer fleet
```

Starts the registry and opens a read-only dashboard in the browser. Sessions are
grouped by working tree by default - so a pooled treehouse slot stands apart from
the main checkout of the same repository - and can be regrouped by repository,
which collapses every worktree of one repo together. Two live agents in the same
working tree are flagged, since that is usually a mistake.

Running `fleet` again while one is already listening opens the dashboard against
it rather than starting a second server. `--no-open` starts the server without a
browser, and the view is linkable: `?group=repo`, `?ended=1`.

The page is one HTML file compiled into the binary. Nothing is fetched from a
CDN, so the dashboard works with no network at all.

## Rooms

A **room** is the shared context for a place agents work: a working tree, or the
repository that owns it. Rooms are part of your engineering process, not the
software being built - nothing here is written to the repository.

```
multiplayer post decision "sqlite over postgres; the Store interface makes it swappable"
multiplayer post question "who owns the retry policy, gateway or client?"
multiplayer inbox --ack
multiplayer resolve 3 "the gateway owns it; client retries are off"
multiplayer room
```

Five kinds, in two groups:

| Kind | Reaches other agents |
| --- | --- |
| `decision`, `finding` | Reference material, shown in the briefing on arrival |
| `question`, `handoff`, `review` | Addressed to somebody; stays open until resolved |

**Sessions join automatically**, to their working tree and to the repository
that owns it. In a primary checkout those are one place, so nothing is said
twice. Set `MULTIPLAYER_AUTO_JOIN=false` to require an explicit `multiplayer
join`.

**Delivery is a notice, not the content.** A session gets a full briefing when
it starts. After that, a `UserPromptSubmit` hook injects one line when something
addressed is waiting:

```
multiplayer: 1 question, 1 handoff unread in this room — run `multiplayer inbox --ack` to read them.
```

That is enough to know something is there, while the decision to read stays with
the agent. The notice does not advance the read cursor, so it repeats every turn
until the agent actually reads - which is what makes delivery guaranteed rather
than one easily-missed mention. It emits nothing at all when there is nothing
unread, when the entry is the agent's own, or when the session is in no room, so
a solo session pays no context for it.

The global skill installed into both harnesses tells agents when reading and
posting are worth doing, and just as importantly when posting would only add
noise.

Answers come back. An agent that asked a question is told when somebody answers
it, even though a resolution is by definition no longer open.

Entries are append-only. Answering posts a new entry pointing at the one it
closes, so nothing is rewritten and concurrent agents never contend for a row.

## State

Entries record that something happened; **state records what is currently
true**, and is replaced in place:

```
multiplayer state set migration/status "tables done, indexes pending"
multiplayer state set migration/status "complete"     # replaces, revision 2
multiplayer state get migration/status                # scriptable, value only
multiplayer state ls build/                           # keys namespace by prefix
```

An append-only log is the wrong shape for a value like this: a reader would
have to fold the history to learn one current fact. State is the other half of
the room, and the division is deliberate - state is *now*, entries are *how it
got here*. If the change itself matters, post a decision alongside it.

Only the last writer, the revision count and the time are kept. State does not
version itself; that is what entries are for.

**Finding things later.** A briefing names what was in the room when a session
started, which is no help twenty turns in. `multiplayer search <term>` covers
state keys and values and entry bodies, ranking a key match above a mention:

```
$ multiplayer search cluster
procedures
  cluster-setup
    1. kind create cluster --config hack/kind.yaml (5 lines)
    multiplayer state get procedure/cluster-setup
```

It matches with `LIKE`, not a full-text index: a room holds hundreds of rows,
and an FTS table would need keeping in step with them, which is the drift a
single source of truth exists to avoid.

**Runbooks are state.** A multi-step process - a cluster update, a release - is
keyed, named and replaced when the process changes, which is exactly what state
is. Key it under `procedure/`:

```
multiplayer state set procedure/cluster-update "$(cat runbook.txt)"
multiplayer state get procedure/cluster-update
```

A briefing names a long value and its line count instead of reproducing it, so
an arriving agent learns the runbook exists without paying context for a body it
may not need. Short values are still stated outright.

## Code review

A review is a batch of findings over a named target, not one large entry:

```
multiplayer review start "comment and CLI verbosity in the store layer"
multiplayer review add r1 --severity must --file internal/cli/install.go --line 23 \
  --symbol trackedEvents "the 3-second fact is stated in four files"
multiplayer review show r1        # markdown report
multiplayer review list --open
```

Findings are ordinary room entries, so each resolves on its own and carries why:

```
multiplayer resolve 1 "fixed: consolidated to install.go"
multiplayer resolve 3 "declined: that sentence is the Upsert contract"
```

**Markdown is the output, rows are the storage.** A report cannot hold what
happened to each finding, and that record - especially *declined, because X* -
is what stops the next reviewer proposing the same change again. `review show`
renders the report on demand, so a stale file can never disagree with the store.

Findings are anchored with `--file`, `--symbol` and `--line`. The line is a hint
only: it rots as soon as the author edits, while the symbol and the commit the
review was opened against are what let a finding be relocated later. Keeping the
anchor structured rather than buried in prose is also what would make promoting
a finding to a pull-request comment possible.

A briefing shows a review as one line with its open count. Thirty findings must
not become thirty lines of briefing.

## Commands

| Command | What it does |
| --- | --- |
| `multiplayer hook` | Record a lifecycle event read as JSON on stdin. Called by the harnesses. |
| `multiplayer ls` | List live sessions. `--all`, `--harness`, `--repo`, `--treehouse`, `--json`. |
| `multiplayer get <key>` | Print one session as JSON. |
| `multiplayer rm <key>...` | Delete session records. |
| `multiplayer fleet` | Start the registry and open the dashboard. Aliases: `dashboard`, `ui`. |
| `multiplayer serve` | Serve the registry API and dashboard over HTTP. |
| `multiplayer post <kind> <body>` | Post to this worktree's room. `--repo` for the wider one. |
| `multiplayer resolve <id> <body>` | Answer or close an open entry. |
| `multiplayer room` | Show the shared context for where you are. |
| `multiplayer inbox` | Entries addressed to you that you have not seen. `--ack` marks them. |
| `multiplayer join` / `leave` | Listen to, or stop listening to, the rooms here. |
| `multiplayer search <term>` | Find runbooks, state and entries by topic. |
| `multiplayer state set/get/ls/rm` | Read and write what is currently true here. |
| `multiplayer review start/add/show/list` | Record and read code reviews. |
| `multiplayer clear` | Delete this room's entries. Lists them unless given `--yes`. |
| `multiplayer prune` | End sessions whose agent process is gone. |
| `multiplayer install` | Register the hooks and the room skill with both harnesses. |
| `multiplayer uninstall` | Remove what install added. `--yes` to act, `--purge` for the store too. |
| `multiplayer version` | Binary version, schema version, store path. |

A session key is `<harness>:<session-id>`, for example `codex:9c81de2a-...`.

## Where records go

By default a SQLite database at `~/.multiplayer/sessions.db`, overridable with
`--db` or `$MULTIPLAYER_DB`.

Setting `--server` or `$MULTIPLAYER_SERVER` points any command - the hook
included - at a running `multiplayer serve` instead, which owns the database on
their behalf:

```
multiplayer serve &
export MULTIPLAYER_SERVER=http://127.0.0.1:8790
multiplayer ls
```

## API

`multiplayer serve` listens on `127.0.0.1:8790` and also serves the dashboard at
`/`. It has no authentication of its own, so it binds to loopback.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Liveness. |
| `GET` | `/v1/sessions` | List. Query: `harness`, `status`, `repo`, `repo_root`, `treehouse`, `limit`. |
| `POST` | `/v1/sessions` | Create or update a session. |
| `GET` | `/v1/sessions/{key}` | Fetch one. |
| `POST` | `/v1/sessions/{key}/end` | Mark ended. Body: `{"ended_at":..., "reason":...}`. |
| `POST` | `/v1/sessions/{key}/touch` | Refresh last-seen. Body: `{"at":...}`. |
| `GET` | `/v1/entries` | Room entries. Query: `room`, `kind`, `open`, `limit`. |
| `GET` | `/v1/state` | Room state. Query: `room`, `prefix`, `limit`. |
| `DELETE` | `/v1/sessions/{key}` | Delete one. |

## Design

```
harness hook ──► internal/hook ──► store.Store ──┬─► sqlitestore  (default)
                      │                          └─► httpstore ──► internal/api ──► sqlitestore
                      ▼                                                  ▲
               internal/detect  (git + treehouse)              internal/ui  (dashboard)
```

**`store.Store` is the seam.** Nothing outside `internal/store/...` knows how
records are persisted. SQLite is the first implementation; replacing it means
satisfying one interface.

**Every implementation passes the same tests.** `internal/store/storetest` is a
conformance suite, not a helper. `sqlitestore` runs it directly, and the HTTP
API runs it through `httpstore`, which proves the server is behaviourally
interchangeable with the local database rather than merely similar to it.

**The hook cannot break an agent.** It exits 0 whatever happens, writing
failures to `~/.multiplayer/hook.log`. Detection is best effort: a session
outside a git checkout, or one whose git call fails, is still recorded, because
knowing an agent is running matters more than knowing its branch. The whole run
is capped at 10 seconds and every git call at 3.

**Location is stored in columns, not a blob.** "Which agents are in this repo or
worktree" stays an indexed query, and the schema lifts onto another SQL engine
unchanged.

### Detection

A git checkout gives the working tree root, the main checkout that owns the
shared `.git`, the branch or detached head, and the origin remote. The two roots
differ exactly when the session sits in a linked worktree.

A treehouse pool slot is found by walking up from the working tree to the
`treehouse-state.json` manifest and matching on the recorded path, rather than
by pattern-matching `TREEHOUSE_ROOT`. A pool relocated with `--root`, or an
in-project pool, is still recognised. The lease holder recorded there is carried
into the session, so a pooled worktree shows both which agent leased it and
which agent session is live inside it.

### Both harnesses, one hook

Claude Code and Codex share a hook wire format: the harness writes a JSON object
to the hook's stdin, and the same `SessionStart` / `SessionEnd` event names
apply. One binary serves both; the hook configuration names the harness with
`--harness`, and `--harness auto` infers it from the environment.

**The dashboard is a client, not a layer.** It reads `/v1/sessions` like any
other client and does its grouping in the browser, so it needs no endpoint of
its own and cannot drift from what the API reports. It is mounted at `/{$}`
only, so it can never shadow an API route.

**The turn hook notifies, it does not deliver.** Injecting entry bodies on every
turn would spend context whether or not it helped, and can derail an agent
mid-task. A notice costs a dozen tokens only when something is genuinely
waiting, and leaves the read to the agent.

Gating the notice on the room having more than one live member looks appealing
and is wrong: an agent that posts a handoff and exits leaves a room of one, and
that is exactly the handoff the next arrival must see.

**Room writes need the local database.** The HTTP store carries sessions only;
`--server` is rejected by the room commands rather than silently writing
somewhere else. The API exposes entries read-only, which is what the dashboard
needs.

**Callers identify themselves from the environment.** Both harnesses put their
session id into the commands they run - `CLAUDE_CODE_SESSION_ID`,
`CODEX_THREAD_ID` - which is exact and, unlike walking the process tree,
survives a harness that runs commands inside a sandbox or through a separate
exec service. Process ancestry and "the only agent here" remain as fallbacks,
and when all of them fail the error names the candidate keys to pass to `--as`.

**Liveness has two halves, and only one is about dead processes.** A harness
that is killed never fires `SessionEnd`, so `prune` reaps sessions whose process
is gone - and it runs automatically when a session starts. Separately, a session
that is alive but idle used to *look* abandoned, because last-seen was written
once at startup and never again; the turn hook now refreshes it, so last-seen
means last active.

Reaping is deliberately timid. A session is only reaped when its pid is absent
or has been recycled by an unrelated command, when the record belongs to this
host, and when it has been quiet longer than a grace period. Where the process
table cannot be read at all - a sandbox, most likely - nothing is reaped, since
"cannot tell" must not read as "everything died".

**Codex sandboxes the commands its agent runs, but not its hooks.** Under
`workspace-write` only the workspace is writable, so a Codex agent could read
its room and register a session while every post, resolve and ack failed with
"attempt to write a readonly database". `install` grants the store directory
write access in `~/.codex/config.toml`, editing the text in place so the file's
comments and ordering survive.

**Identity comes from the environment, but the first match is not always ours.**
A harness launched from inside another inherits its variables - running Codex
from a Claude session leaves `CLAUDE_CODE_SESSION_ID` set in Codex's commands -
so every advertised identity is checked against the registry, and where several
are registered the one in this room wins.

**A room has two halves on purpose.** Entries are append-only history, which is
what makes concurrent writes safe and resolution a new row rather than an edit.
State is keyed and overwritten, because "the migration is half applied" is a
value, not an event, and folding a history to read one value is the wrong shape.
Mixing the two into one mechanism would make one of them awkward.
