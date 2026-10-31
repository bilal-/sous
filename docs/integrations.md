# Native task integrations

sous supplies task state to a terminal host such as herdr. The host renders
its own task list, project summaries and notifications. sous keeps the notes,
discovers the work, and performs the person's requested actions through its CLI.

The interface is `sous.integration`, version **0**. It has its own version,
separate from the command's release, the four plugin axes and state files.
Version **1** is a draft beside it; see [Version 1](#version-1-draft).
The complete herdr plugin is included in
[`integrations/herdr`](../integrations/herdr/README.md): a Tasks pane, sidebar
summaries, notifications, project links and CLI actions. It consumes this
interface using herdr's existing APIs. Other hosts can use the same contract.

## Discover sous

A host resolves `sous` on the machine's `PATH`, then runs:

```sh
sous integration --json
```

This answers even before setup or when configuration cannot be read. It reports
the protocol version, executable version, snapshot and subscription commands,
context and refresh commands, and task actions. Reject an unsupported `v`;
ignore additional fields within a supported version. Keep the resolved
executable as the first argument when invoking the advertised commands.

For repository discovery, start at
[`integrations/herdr/provider.json`](../integrations/herdr/provider.json).
It points to the discovery command, this guide, and
[`integrations/schema.json`](../integrations/schema.json). The schema describes
discovery records, runtime descriptions, snapshots, stream events and errors.
The herdr plugin's install entrypoint is
[`herdr-plugin.toml`](../integrations/herdr/herdr-plugin.toml). The JSON discovery
record is the host-neutral provider contract, not a herdr install manifest.

## Read a snapshot

```sh
sous integration snapshot --json
```

This is offline. It reads the last full board, overlays current private notes
and snoozes, checks local filed notes and built in runners, and checks whether
known project folders still exist. It launches no refresh, signal scan or agent.
Remote trackers and runner plugins retain their last known answers.

The response has `kind: "snapshot"`, `v: 0`, `provider: "sous"`, and:

| Field | Meaning |
|---|---|
| `revision` | Opaque state revision; compare it for equality. Poll timestamps do not change it. |
| `configured` | Whether project roots are configured. |
| `available` | Whether a structured full board has been saved. |
| `complete` | The saved picture is available, configured and has no known gaps. |
| `as_of` | Time of the last full board, or `null` before one is available. |
| `observed_at` | Time current local state was read. This does not make remote data fresh. |
| `items` | Flat task list, including ideas and snoozed items. |
| `projects` | Projects from the last full board. Items can also name projects outside that list. |
| `sources` | Each saved signal source's status and error. |
| `problems` | Reasons the picture is incomplete; keep them visible alongside counts. |
| `checked`, `unavailable` | Counts from the last full board. |

Items carry the same fields as the board's JSON read model, plus `key`,
`section` and `actions`. `section` is `on_you`, `on_others`, `unfinished`,
`ideas` or `snoozed`. A default attention list shows the first three. Keep
ideas and snoozed items available when the person opens a project.

Use `key` for list identity and diffing: `n:<uid>` for a private note or run,
and `s:<id>` for a signal. Use `id` for CLI actions. Namespace keys by the
host's machine and sous instance; separate machines can have identical paths.
Use the full `project` path for matching, and `name` for display.

`run.state` is `starting`, `running`, `needs_you`, `done` or `failed`.
`done` means a result is ready for review. Closing the note remains an explicit
action after checking the work. A signal has no note-closing action.

If roots are configured but there is no structured board, the command exits
`3` with the usual JSON error and a pointer to `sous --refresh`. An empty list
is not returned as a substitute for missing data. A corrupt or newer state
file exits `1` and is never overwritten. Without roots, a snapshot reports
that setup is needed and keeps any known private notes.

## Subscribe to changes

```sh
sous integration watch --json
```

The host owns this child process. It receives one complete JSON object per
line, starting with a baseline. The process checks local state once per second;
it emits only when the revision or availability changes. It reloads
configuration between reads. Stop it with SIGINT or SIGTERM when the host
disconnects. Closing stdout stops delivery at the next emitted event.

| `type` | Host behavior |
|---|---|
| `snapshot` | Replace state with `snapshot`. It is the initial baseline or recovery after an error. |
| `changes` | Replace state with `snapshot`; `changes` describes item additions, updates and removals since `previous_revision`. |
| `unavailable` | Keep the last known state and mark it unavailable. `error` explains why; `exit` is `1` or `3`. |

Every event has `kind: "event"`, `v`, `provider`, `type`, `revision` and
`changes`. Snapshot and recovery events have an empty `changes` array.
Changes have `type: "item.added"`, `"item.updated"` or `"item.removed"`, a
stable `key` and CLI `id`. Additions and updates include the current `item`.
Removal means the item is no longer in this projection; it alone does not
prove that someone completed the work.

The stream describes current state, with no durable replay cursor or event
history. Changes between reads may coalesce. After a broken pipe, lost
process, or unsupported event, reconnect and accept another baseline.
An availability failure does not produce synthetic removals. Recovery sends
a full baseline so the host can reconcile safely.

## Herdr integration

The bundled plugin supplies:

* An interactive Tasks pane across all projects, with a current-Space filter,
  ideas and snoozed views, details, private note capture and task actions.
* Project counts and freshness in herdr's sidebar using workspace metadata.
* Linked project panes, with task titles in agent metadata, and focus reuse
  for a task already open in the same sous instance.
* An observer tied to the herdr server's lifetime, with quiet baselines,
  notifications for task transitions, offline updates and optional remote
  refresh on an explicitly configured interval.

Follow [the installation and user guide](../integrations/herdr/README.md).
All plugin code and tests live here. Herdr supplies the terminal pane, sidebar
and notification APIs; the plugin owns the task view and observer.

A host implementing additional native surfaces can use the same interface for:

* A native Tasks list across every project, including projects without an
  open herdr Space. The current workspace can filter that same list.
* Space summaries computed from sections, with `problems` and `stale` kept
  visible whenever counts are incomplete.
* Task-to-pane links. Store the item's stable key as herdr pane metadata;
  match it before starting a second agent. Use `context` with a pane's cwd
  to resolve a project or worktree through sous's existing `here` view.
* Notifications when an item newly needs the person: a new `on_you` item,
  or a run changing to `needs_you`, `done` or `failed`.
* Task details, private note capture, snoozing, closing a checked note,
  opening a project and answering a run from herdr's own controls.

Establish the initial baseline quietly. Compare stable keys and meaningful
fields for notifications. A changed source timestamp, a polling timestamp,
or a recovery baseline should not notify every task again. After recovery,
the host can compare its retained baseline with the new snapshot. Keep
pending work visible in the task list when a toast could not be delivered.

Herdr's plugin v1 renders plugin views as terminal panes. The bundled plugin
uses that surface together with workspace/pane metadata and notifications.
Herdr does not accept task navigation targets in `notification.show`; its alerts
direct the person to Tasks. A host exposing native widgets can render the same
task contract in those widgets.

## Invoke actions

The discovery response advertises actions by ID. Each item lists its applicable
action IDs. `note` is a project-level action. Bind each parameter to an entire
argv element, and set an action's `cwd` when it is present. Never join an argv
array into shell text. Paths, answers and note text are literal values;
the templates put user text after `--`.

The `open` action needs a real terminal: herdr starts it in a managed pane so
the agent gets sous's project handoff and owns terminal input. `reply` is
advertised only when a run is known to need an answer. Refresh the snapshot
before offering an action from an older view, and handle ordinary CLI errors
when the underlying state changed meanwhile.

`effect` distinguishes reading, launching and writing private state.
The person requests actions; a stream event does not authorize
closing a note, replying, or starting an agent. Public filing, upstream
closure and run cleanup remain separate explicit CLI operations.

Use the advertised `refresh` operation for a full remote scan, outside UI
startup and workspace focus. Its successful stdout is empty. Reconcile the
result through the subscription; a failed refresh keeps the last known
picture. A host may offer a Refresh button or manage a refresh schedule,
independently of the offline subscription.

The `context` and action outputs follow their existing command guides in
[`docs/commands.md`](commands.md). Their errors are one JSON object on stdout
and a clear line on stderr, with the command's exit code. The `watch` stream
is the explicit exception to the usual single-value JSON output.

## Version 1 (draft)

Version 1 adds what a phone or any remote host needs to start work and
follow it: run phases, progress, questions, results, starting and stopping
runs, approving a result, and operation ids that make every action safe to
repeat. A host asks for it with `--protocol 1` on every call:

```sh
sous integration --protocol 1 --json
```

The answer lists `versions` (every version this sous answers) and
`draft`. While `draft` is `true`, version 1 may still change, and sous
fills it in release by release; build against
[`integrations/schema.v1.json`](../integrations/schema.v1.json), which is the
whole of it, and the examples in
[`integrations/examples/v1`](../integrations/examples/v1). Its advertised
commands carry `--protocol 1`, so a host that runs them as given stays on
version 1. Without the flag, sous answers version 0 exactly as before, and
version 0's description lists `versions` so a host can find version 1.

What version 1 changes, beside version 0:

* **Actions** declare a `scope` (`global`, `project` or `item`), typed
  `parameters` (`id`, `project`, `string`, `text`, `commit`, `question`,
  with `max_bytes`), `input: "stdin"` when the one text parameter goes on
  standard input, `confirm` when the host must ask first, and `op` when the
  action takes `--op <op>`. `effect` gains `write_public`; a host refuses
  to run an effect it does not know.
* **Runs** carry `phase`, `reason`, `progress`, `question`, `result` and
  `publication`; `error` already says the runner could not be asked this
  time, and `checked_at` how old the answer is. Derive "needs you", "in progress" and "ready
  to approve" from `phase`; `section` keeps its version 0 values. An
  unknown phase is shown as reported, never as finished.
* **Outcomes**: every action that takes `--op` prints one `outcome`
  object. The same `op` with the same input gets the same outcome
  (`already_done` when it was done before); the same `op` with different
  input is `refused` with `op_reused`. Done, conflict and a refusal of the
  input itself (`bad_input`) are final. A failure, a refusal that depends
  on settings (`not_allowed_project`) and `not_ready` are not: the same op
  may be tried again and may come out differently. sous retains each input binding and final
  outcome. A per-op process lock returns `in_progress` while its request is
  alive; elapsed time alone never authorizes another execution. After an
  interrupted request releases the lock, the same input can recover using the
  action's durable receipt. An unresolved operation from an older sous returns
  `not_ready` with `legacy_operation`: inspect its result before starting a new
  action. Waiting does not resolve it.
* **Ops** are 16 to 64 letters, digits, `-` and `_`, made by the host, one
  per thing the person asked for. Fill `{op}` in an action's `argv` with it.
  Actions include `run` (a project allowing `remote_runs`, brief on standard
  input), `stop`, `reply` (question `<id>:<token>` from `run.question`),
  `approve` (the reviewed commit, with confirmation before a public write),
  `discard`, `done` and `snooze`. Use each item's advertised actions. `diff`
  is a paginated read with no op. A run's question always has a token, derived
  from its words when the runner does not report structured questions.
  `done` stops any run before closing the private note; it never closes a
  tracker item or filed checkbox. `snooze` hides a note for seven days, or a
  signal until its content changes. Private mutation receipts are committed
  with their target state: a retry preserves the original deadline and signal
  identity, even if the outer outcome was lost. It never hides changed work,
  resolves an old prefix to a new signal, or undoes a later snooze or closure.
* **Watch** asks runner plugins how their runs are going every 5 seconds
  (version 0 only reads what they said last), so its `offline` is false.
  It sends changed items only after its first snapshot, with a
  `meta` object for board fields that changed (each replaces the one held;
  absent ones did not change). With `--max-bytes <n>` (at least 16384) no
  line is longer: a baseline leaves out the oldest items of the lowest
  sections first (snoozed, ideas, unfinished, on others, on you) and counts
  them in `omitted`, and a change too long is sent as such a baseline. The
  revision is always the whole board's. A host never takes a left-out item
  as removed; it may later arrive as `item.updated`, which a host that
  does not hold it adds. If metadata or an error still cannot fit after
  trimming, the watch ends with exit `1` and one compact JSON error line
  explaining the limit. Reconnect with a larger limit; this terminal error
  is distinct from a recoverable `unavailable` event. The byte count includes
  the newline.

## Design: tokens and display rules

Every surface draws the same JSON with its own components. What makes them
one product is [`integrations/design`](../integrations/design/README.md):
`tokens.json` (colours, the colour and word for each group, type, radii,
spacing) and the display rules (group order, which phase goes in which
group, plain words, what Approve must show, ops). `sous serve`'s page
follows them; so should any other host.

## Contract checks

`make ci` covers discovery without setup, unavailable and newer state,
current notes and snoozes, offline run reconciliation, stable identity,
change suppression, failure recovery, literal action arguments and a real
watch process against a throwaway home. No test contacts herdr, a tracker,
or an agent for real. Adding an action or changing the wire format requires
updating the schema, this guide and the command help together. The v1
examples are validated against `schema.v1.json` by the same tests.

## Effort summaries and project memory

Snapshots can include an optional `efforts` array of active effort summaries:
`id`, `name`, `projects` (workspace/project/remote scopes), `open`, `completed`,
`blocked`, `unavailable`, `stale`, `archived` and `attention`. Counts describe
explicitly linked commitments. They do not change task ownership or status.
Version 1 watch metadata includes `run_projects` when the projects permitted to
start runs change; an empty array revokes all of them. It also includes `efforts`
when it changes; an empty array clears
previous summaries. Its contents participate in the snapshot revision.

These fields are additive to both existing snapshot protocols. Hosts that do not
render efforts can ignore them. Existing task actions and the plugin contract
are unchanged. Private memory bodies are not included in snapshots. A project
brief (`sous here --json`) has a `context` object with project-scoped memories,
relevant efforts and source failures. An effort link alone does not share a
memory into another project's brief.
