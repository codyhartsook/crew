# Architecture

Every message costs context in each agent it reaches.

> Route information to the narrowest channel that reaches its audience.

## Channels

| Channel | Reaches | Cost | Use when |
|---|---|---|---|
| Room post | every agent, now and later | Highest | it changes another agent's work |
| Addressed request | one agent, pushed by the broker | One turn | handing one agent a task |
| Notice | one agent, no payload | One line per turn | unread work exists |
| Role skill | every session's skill listing | One line per holder | the user gave an agent a role |

Arrival shows the agent's name, room, and who's here, plus counts of entries, never the entries themselves.

## Roles

```mermaid
sequenceDiagram
    actor User
    participant T as moss-otter (tester)
    participant C as crew
    participant P as Peer agent

    User->>T: "you're the tester"
    T->>C: crew role tester "<description>"
    C-->>P: crew-role-moss-otter skill (next turn)
    P->>C: crew post request --to moss-otter
    C-->>T: broker push
    T->>C: crew resolve <id>
```

## Role skill lifecycle

```mermaid
flowchart LR
    store[("role_assignments<br/>+ active sessions")] --> sync{{sync}}
    sync -->|role held| write["write crew-role-&lt;alias&gt;<br/>~/.claude/skills, ~/.codex/skills"]
    sync -->|session gone| remove["remove crew's copy<br/>(edited copies kept)"]
    hooks["every hook"] --> sync
    sweep["broker liveness sweep"] --> sync
```

| Holder ends by | Skill removed by |
|---|---|
| Normal exit | SessionEnd hook |
| Crash | Broker sweep, or the next session's hook |
| `crew role --drop` | The command, or the next hook if a sandbox blocks it |

## Presence

A process id isn't a session, so liveness comes from each harness's records.

| Harness | Live when |
|---|---|
| Codex | its thread holds a flock writer lock |
| Claude | its terminal process runs (one conversation per process) |
| Either, with no usable records | its pid runs |

A timer ends closed sessions and adopts unseen live ones.
