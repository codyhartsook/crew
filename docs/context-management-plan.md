# Implementation plan: sub-agents and channel separation

Delivery order for the architecture in context-management.md. Each phase ships
something usable on its own, so the work can stop at any boundary.

## Before starting

Read context-management.md first. It carries the principle the phases below
serve: route information to the narrowest channel that reaches its audience.

- New CLI commands follow the existing internal/cli/<name>cmd layout.
- Harness flags quoted here were verified against claude 2.1.270 and codex
  0.154.0 on 2026-09-14, by running --help rather than reading docs. If a flag
  is missing, check --help for the installed version before assuming the plan is
  wrong.
- Phase 0's Codex trust item is a suspected live bug. Reproduce it end to end
  against a clean CODEX_HOME before changing anything, so the fix addresses the
  real cause.
- Every phase lands with tests. The exclusion tests in phase 2 matter most,
  since a leak there is silent and world readable.

## Phase 0: foundations and one likely bug

Small, independent, unblocks confidence in everything after it.

- Verify Codex hook trust. Run crew init against a clean CODEX_HOME and confirm
  hooks actually fire. Untrusted hooks are dropped silently, with no error, and
  trust is keyed in config.toml as `<abs hooks.json path>:<snake_event>:<matcherIdx>:<hookIdx>`
  with a trusted_hash. If init does not write that entry, Codex sessions never
  join rooms on a fresh install and nothing reports it. Touches
  internal/cli/initcmd/init.go and internal/cli/hookconfig.
- Check the Codex SessionEnd timeout in the harness Timeouts map against the
  3s clamp Codex applies.
- Raise the posting bar in the hint at internal/room/render.go:69-71. It
  currently carries mechanics with no threshold, while the threshold lives in
  the skill, which loads on demand. Over-posting follows from that asymmetry.
- Rename the environment prefix from MULTIPLAYER_ to CREW_, matching the binary
  users actually type. Four constants in internal/cli/cmdutil/options.go:18-21
  plus MULTIPLAYER_RELAY_MARKER in the Claude notifier tests, so the change is
  mechanical. No compatibility shim: change it once rather than carrying both.

## Phase 1: role definitions

The registry everything else reads. No behavior change.

- Loader discovering both scopes: repo .crew/agents/*.toml and global
  ~/.crew/agents/*.toml, repo winning on name collision.
- Validation with clear errors. A bad definition must fail at load, not at
  spawn, and the error must name the file.
- `crew roles` to list what is defined, from which scope, and what is active.

Shape:

```toml
# .crew/agents/tester.toml
name = "tester"
description = "Runs the full test suite and reports failures"
harness = "codex"             # claude | codex | any
memory = "role"               # role | session | none
# Top-level keys must come before the first [table] header below - TOML
# would otherwise nest a trailing "instructions" inside [output].
instructions = """
Run the suite. Write full output and diagnosis to role memory.
Return only: pass and fail counts, failing test names, one line cause each.
"""

[triggers]
prompt = ["run (the )?tests", "do the tests pass"]
tool = [{ tool = "Bash", pattern = "^(go test \\./\\.\\.\\.|npm test)\\s*$" }]
mode = "suggest"              # suggest | enforce, see phase 6

[render]
model = "gpt-5.3-codex"
sandbox = "workspace-write"   # codex
tools = ["Read", "Grep", "Glob", "Bash"]   # claude
effort = "medium"

[output]
schema = "schemas/test-result.json"
```

The render block deliberately carries fields for both harnesses rather than a
lowest common denominator, since Claude has no sandbox mode and Codex has no
tool allowlist. Each spawn path takes what applies.

## Phase 2: role memory

Standalone value: useful before delegation exists, since any agent can carry a
role identity.

- New table keyed (room, role), separate from entries. Not a visibility flag.
- `crew memory` read and write.
- Tests asserting exclusion from Briefing, Notice, and the generated room
  snapshot. The snapshot is world readable, so a leak there is the expensive
  failure. Touches internal/room/render.go and internal/roomdoc.
- Key derivation falls back to session scope where a room defines no roles, so
  the policy stays one code path.

## Phase 3: roster injection, routing tier 1

- Bind definitions to rooms: which roles are active where. Definition and
  activation stay separate, and activation is explicit opt-in.
- Grow crew init an interactive step to review discovered roles and select which
  are active, alongside the existing report and install flow in
  internal/cli/initcmd/init.go. Init is already foreground and interactive, so
  this fits without changing its lifecycle.
- Inject the active roster as additionalContext on SessionStart. Both harnesses
  accept the same envelope. Branch in internal/cli/hookcmd/rooms.go:59-80.
  SubagentStart is not used: crew-spawned roles are fresh sessions, and native
  sub-agents should not be invited to delegate further.
- Start the routing decision log here, including whether the agent took the
  suggestion. It is cheap now and impossible to reconstruct later.

## Phase 4: delegation spawn mechanics

The core capability. Build --wait first, since blocking needs no persistence.
Async lands in phase 5 and becomes the default.

- `crew delegate <role> "<task>" --wait`, returning a bounded result.
- Spawn headless with auto-join disabled, role and delegation id on the
  environment. Env carries correlation because Claude accepts an injected
  session id and Codex does not, so neither harness's own id is a usable key.

```sh
CREW_AUTO_JOIN=0 \
CREW_ROLE=tester \
CREW_DELEGATION=<id> \
claude -p "<task>" \
  --append-system-prompt "<instructions>" \
  --model <model> \
  --json-schema '<schema>' \
  --permission-mode acceptEdits \
  --permission-prompts none \
  --allowedTools "Read,Grep,Glob,Bash" \
  --output-format json
```

```sh
CREW_AUTO_JOIN=0 \
CREW_ROLE=tester \
CREW_DELEGATION=<id> \
codex exec "<instructions>\n\n<task>" \
  -m <model> \
  -s workspace-write \
  --output-schema <schema file> \
  -o <result file>
```

Asymmetries that are not mistakes: Claude takes the schema inline and Codex
takes a file path, so crew writes a temp file for Codex. Claude gets the persona
as a system prompt and Codex gets it composed into the prompt, because codex
exec has no system-prompt flag. Claude needs explicit permission flags because
-p starts in manual mode; Codex already defaults to approval never.

Never pass --bare to Claude, which skips hooks entirely, or --ignore-user-config
to Codex, which skips the file holding hook trust.

## Phase 5: asynchronous delegation, and the default

- delegations table: id, room, role, requester, prompt, status, result.
- Fire and forget becomes the default, with --wait opting into blocking.
- Result retrieval, and surfacing through the existing notice path rather than a
  room post.

## Phase 6: routing tiers 2 and 3

- PreToolUse interception per role, opt-in, with carve-outs so targeted work
  passes through. Deny with reason works identically on both harnesses. Gate the
  loop guard on agent_id presence on Claude; Codex exposes no in-subagent
  identity, so keep enforcement advisory there.
- Adjudication behind a deterministic prefilter, or async on the next notice.

## Definition versus delivery

The crew definition is the only source of truth for a role. Harness-native agent
files are a delivery mechanism, and we do not need them.

- codex exec has no --agent flag and no system-prompt flag, so .codex/agents
  files are unreachable from non-interactive mode. Deliver the persona by
  composing the prompt as instructions plus task. The positional argument is
  documented as the agent's initial instructions, so this is the intended seam.
  Use -p <profile> for the config half if model and sandbox want separating.
- Claude takes --append-system-prompt, which lands the persona in the system
  prompt exactly where a native agent file would, with no file to keep in sync.

Generating .claude/agents files remains worthwhile later, but only so a human can
at-mention a role in an interactive session without going through crew. It is not
on the critical path, and it has no Codex counterpart.

Spawn flags worth using: -o writes just the final message, so the return path
does not parse JSONL; --output-schema takes a file path, so the schema is written
to a temp file at spawn; --ephemeral suits throwaway role spawns. Never pass
--ignore-user-config to a spawned role, since hook trust lives in that file and
skipping it disables crew's hooks silently.

## Decisions

Settled, with the reasoning, so they are not relitigated mid-implementation.

**Definitions live in two scopes.** Repo files at .crew/agents/*.toml, and
global at ~/.crew/agents/*.toml. Repo wins on name collision, since more
specific wins. Repo definitions are version-controlled and reviewable, which is
the point of preferring them.

**Activation is explicit opt-in.** A discovered role is dormant until a room
enables it, because the roster is injected into every session in that room and
an unfiltered roster spends that context on roles nobody uses. crew init grows
an interactive step to review and select which roles are active, alongside the
existing report and install flow.

**crew delegate is asynchronous by default**, with --wait to block. Fire and
forget suits long work and parallel fan-out; the result surfaces through the
existing notice path rather than a room post.

**Role memory is a SQLite table** keyed (room, role), not files. Memory is
coordination state, not a document. Two reasons beyond consistency:

- Concurrency is designed in. Keying by role rather than session means parallel
  instances of a role share one stream, so simultaneous writes are expected. A
  table makes that a transaction; a file would need crew to implement locking.
- Writes need a chokepoint. Direct file editing lets a role append without
  bound, and the next spawn's memory pull then blows up the context this design
  exists to protect. Routing writes through crew memory gives one place to
  enforce size limits and compaction.

A generated read-only view, in the style of roomdoc rendering ROOM.md, is
optional polish if inspection ever needs it. Not part of the design.
