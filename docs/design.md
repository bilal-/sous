# How sous is designed

## The problem

AI tools made doing the work much faster. One person can now keep ten or
twenty projects moving instead of two or three. The hard part moved from
"can I build it" to "can I keep all of it in my head".

Switching between projects has two halves:

| Half | The question | What usually answers it |
|---|---|---|
| **Choose** | Which of my projects has something for me? | Each tool's own dashboard (GitHub, GitLab), which only knows its own items and has to be opened. |
| **Resume** | Now that I am here, where was I? | Notes, issues and commit history, if someone wrote them. |

A note inside a project cannot remind you that the project exists. Anything
you have to remember to open has already failed.

## What sous is

**A personal list of what is waiting on you, across every project and
tracker you use, that explains itself and gets you back into the project
you pick. It never takes over how you work.**

* **An index of live work, with private memory.** Work lives in git, on GitHub
  or GitLab, or in the project's `FOLLOWUPS.md`. Sous keeps pointers, personal
  notes and sourced context. Efforts link those existing commitments across
  projects; they do not create a second task status system.
* **Personal, not shared.** It is one person's inbox and list of things
  they are waiting for. When a note should be seen by others, you *file* it
  into the project's tracker on purpose, and sous keeps a pointer to it.
* **Promises first, leftovers second.** The headline is about things with a
  person on the other end: reviews asked of you, notes marked *on you*. Work
  left in git (uncommitted files, unpushed commits, stashes) is shown too,
  with its reason, and can be snoozed.
* **It explains itself.** Every row says why it is there and how long it has
  been waiting. Missing data never looks like zero: a source that failed, a
  folder that is gone or a row that is out of date says so.
* **No API key, no background service.** Anything that needs an AI is done
  by the agent you already use, which runs sous like any other command.
  `sous serve` runs only while you keep it open.
* **It hands work off, and keeps track of it.** Your agent can hand a task
  to a runner, which works on it in the background. sous shows how it is
  going, and tells you when it is done or needs you. Work you handed off
  is one more thing waiting on you, so it belongs on the same list.

## What sous is not

* **Not an orchestrator.** It hands work to runners and shows you what
  they report. The built in runners start one agent and do nothing more:
  retrying, reviewing and scheduling belong to a runner such as orchid.
  `sous go` without `--run` starts your agent in a project and steps aside.
* **Not a tracker.** No priority, status, assignee, sprint or estimate.
  Age and kind are the only order.
* **Not a registry.** You never add projects. sous finds them by looking
  through the folders you point it at.
* **Not an assistant.** It has a short, fixed set of commands. Understanding
  plain language is your agent's job.

## Write where you are, read when you forget

* **Writing happens in context.** You are in the project, so asking you to
  run `sous note` is fine.
* **Reading happens out of context.** This is where "it must work when you
  forget it" matters. The board prints when a shell opens, is shown to an
  agent when a session starts, and sits in the menu bar.

## Commands

Four plugin axes support live work. Private memory and efforts have their own
commands and local state, without introducing another plugin axis:

| Commands | Answers | Grows through |
|---|---|---|
| `sous`, `here`, `<project>`, `projects`, `report`, `review` | What is waiting on me? Where was I? What changed? What needs checking? | **signals** |
| `note`, `edit`, `kind`, `snooze`, `done`, `file` | Remember this, for that project | **backends** |
| `go` | Take me there | **launchers** |
| `go --run`, `show`, `reply`, `approve`, `discard` | Do this for me, and tell me how it went | **runners** |
| `remember`, `recall`, `memory` | What did we learn or decide, and what supports it? | Private memory |
| `effort` | Which projects and commitments belong together, and what is blocked? | Private effort links |

Plain `sous` always shows the board. `here`, the session hooks and `go` stay
on your machine and finish quickly: they never wait on the network.

## Command output and updates

The CLI follows the ten AXI principles pinned in AGENTS.md, with the documented
choices of plain text plus JSON, stderr diagnostics plus JSON errors, and
short output with explicit full-record commands rather than `--fields` or
`--full`. The bare board's header names this executable and its purpose.
Search totals count all matches before limiting; truncation is explicit.
Contextual commands carry the selected project, with shell-safe quoting.
Invalid selectors and oversized memory input fail without a partial write.

`sous update` is an explicit maintenance command. It checks the latest stable
GitHub release, verifies release checksums and atomically replaces a downloaded
binary under an installation lock. Homebrew owns upgrades to its installations.
A source build stays under the checkout owner's control. No downgrade or
background update occurs. `--check` is read-only; failed checks remain failures,
not an assertion that the installed version is current. The updater never
runs setup or opens private data files.

## Native hosts

A host such as herdr can discover sous's task operations with
`sous integration --json`, read an offline snapshot and subscribe to local
changes. The host owns its task interface, project navigation, notification
policy and the child process's lifetime. Sous remains the source of task
state and applies actions through its existing commands.

The subscription overlays current private notes, signal snoozes, local
filed-note status and built in runner reports on the last full board. It
does not scan signals or contact remote trackers and external runners.
The last refresh time stays visible, missing data stays explicit, and a
failed read preserves the last known revision. Stable keys identify tasks
across snapshots; their CLI IDs address actions. A change feed projects
current state and adds no event journal, daemon or scheduler.

The bundled herdr plugin is a standalone host CLI in `integrations/herdr`.
It renders Tasks in a managed terminal pane and reports summaries and alerts
through herdr's APIs. Its observer and versioned cache belong to that plugin,
live in herdr's plugin state directory, and stop with the herdr server or when
the plugin is disabled. Sous has no detached observer or service of its own.
The host plugin owns any explicitly configured refresh interval.

`sous serve` is one more host of the same contract: a web page on this
computer that runs the same `sous integration --protocol 1` calls, so it and
Shellbell's phone screen draw the same JSON. It is not a service: it runs
while you keep it open, listens on 127.0.0.1 only, and lets in only the
browser that opened the address it printed (a secret in the address sets a
cookie; the Host and Origin of every request are checked). The look the two
share is data, not code: [`integrations/design`](../integrations/design/README.md)
holds the tokens and display rules each surface follows with its own
components.

The native host contract has its own protocol version. Its guide, schema
and host discovery records live in [integrations.md](integrations.md) and
the repository's `integrations/` folder. Hosts execute action argument lists
directly and invoke writes on the person's request. Private notes become
shared only through explicit filing. A host may offer a separate refresh
action; session startup and the subscription remain offline.

## Data

Local state lives in `~/.sous/` (or `SOUS_HOME`); filed notes live in their
selected backend. Each sous-owned JSON state file outside `runs/` has a
version number, is saved whole under a lock so a crash never leaves half a file, and is
upgraded when an older one is read. A file written by a newer sous is
refused, never overwritten.

The files, and what each holds, are listed in
[the command guide](commands.md#files-sous-keeps); a run's folder under
`runs/` is not versioned, since it belongs to its runner.

Live work is worked out again on every full look. Private notes, memories and
effort links persist independently of those observations.

## Keeping a project ready to resume

sous encourages three habits: capture unfinished work, leave a useful
handoff, and reconcile what remains. Project documents belong with the
project. sous points to them; it does not keep another copy or require a
particular set of files.

`here` discovers existing `README.md`, `AGENTS.md`, `CLAUDE.md`, `STATUS.md`
and `FOLLOWUPS.md`, including names with different capitalization. A
`STATUS.md` is a short current handoff: what works, what was verified,
blockers and the next action. `FOLLOWUPS.md` keeps unfinished actions,
their reason and useful references. Working instructions belong in
`AGENTS.md` or the project's equivalent; important decisions belong in its
design documents. Existing conventions win. Missing documents are optional;
unreadable documents are shown as unavailable.

Agents read these documents when resuming work and keep the handoff current
as part of authorized changes. Creating a shared document or publishing a
private note still requires the person's explicit request. Agent guidance
lives in `sous help`, not the skill.

## Durable context and connected work

Project memory records a handoff, decision, constraint, lesson or open question
with its author, capture time and optional source reference. Verification is an
explicit writer assertion with evidence and a timestamp, never automatic task
completion. A changed decision supersedes its predecessor; recall hides the old
one by default but keeps the chain. Forgetting removes the body and evidence,
retaining a tombstone and fingerprint so replay cannot bring the text back.

A memory belongs to a project and workspace. Workspace defaults to the most
specific discovery root, or the project's path when unconfigured; the per-project
`workspace` setting can name it explicitly. Automatic context matches both the
workspace and project (path or remote), plus explicit sharing grants. Workspace
and global searches require explicit scope options. These are context boundaries,
not security against programs that can read the user's files. A replacement does
not inherit sharing grants. Source references are pointers, never instructions
for sous to fetch or execute.

Explicit project names retain their discovered scope, including symlink aliases.
An explicit filesystem path retains its physical checkout scope. Current-directory
selection (including `.`) checks local discovery aliases and refuses to choose if
any has a different private scope. Incomplete discovery or invalid configuration
makes implicit context unavailable. Briefings still show local project facts and
notes, with an attention message and no private memory or effort context. No
selection rewrites stored scopes, retry keys, history or sharing grants. Old
records remain accessible by ID; a history hint that cannot select the stored
project exactly is labelled as a broader workspace-history query.

Efforts collect project, note, memory and document links under a name. Their open,
completed and blocked counts come from current notes and cached tracker status.
Dependencies only explain what waits on what. Missing notes, projects or sources
remain visibly unavailable. Archiving an effort changes no commitment. Linking a
memory into an effort does not share its body with every project's session.

The board shows active effort summaries. Project briefings include relevant
memories and efforts, limited to five of each in the brief form. The newest of
each memory kind gets a place before remaining slots fill by recency, so repeated
handoffs do not bury a standing constraint. JSON retains full context. All reads
are local; no model endpoint, embeddings, transcript extraction or daemon is
required. Agents capture useful context during authorized work and cite the
project's own documents. Keyword retrieval remains explicit about its limits.

## Reviewing unfinished work

`sous review` refreshes the board and selects notes not reviewed for seven
days, missing or unavailable filed items, and runs that finished or failed.
Snoozed notes, active runs and notes kept within the last seven days are
left out. The board and `here` show a small reminder when a review is due;
there is no separate scheduler or
background service. The review JSON includes the last session as context,
not proof that a task is finished.

Only confirmed tracker closures are automatic. Completing private work
requires the person or their agent to verify the task and close its note
with `done`. Age, missing data and an agent's summary never prove completion.
`review --keep <n>` records that the note still matters, restarting the
seven-day review window without hiding it or changing its creation time.
Closed notes stay in history.

Cached shell, session-summary and menu-bar views read current local notes
alongside their saved remote signals. A local closure, edit, snooze or
review therefore takes effect without waiting for a network refresh. The
remote snapshot keeps its original time and any uncertainty.

## Keeping filed notes in step

When the board is built, sous asks each filed note's tracker whether the
item is still open. `here` asks only trackers that live in the project,
such as `FOLLOWUPS.md`: a note filed on GitHub or GitLab shows there with
its ref, and the board is where its status is checked.

If the tracker says *closed*, the note closes in sous too, marked
`closed_by: upstream`. The tracker wins, and the record of who closed it is
kept. If the item cannot be found, the note shows `(ref missing)`. If the
tracker could not be reached, it shows `(status unavailable: ...)`. Neither
is ever taken to mean closed.

## Plugins

Each group grows through one kind of plugin. A plugin program is named
`sous-<kind>-<name>`. Built ins run as separate processes through
`sous <kind> <name> <call>`. Both use the same argument, standard input,
standard output and exit-code contract. Calls have bounded time limits;
an interactive launcher replaces sous and owns the terminal.

* **signal** finds what waits, one JSON line per finding.
* **backend** files a note in a tracker and says whether it is still open.
* **launcher** starts an agent or editor in a project, taking over the
  terminal.
* **runner** takes a task, works on it somewhere else, and says how it is
  going.

Every call, its input and output, and the exit codes all four share are in
[plugins.md](plugins.md), the one place the contract is written down.

Plugins from others run only when listed in `config.toml`.

## Versions

* The command follows semantic versioning. Before 1.0, flags may change
  between minor versions.
* The plugin contract carries a version field, and freezes before 1.0.
* Sous-owned JSON state is upgraded forward; newer versions are refused
  by older sous rather than overwritten. Runner files have their own formats.

1.0 is a promise that the plugin contract is stable, not a milestone of
popularity.
