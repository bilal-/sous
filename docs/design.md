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

* **An index, not a store.** Work lives in git, on GitHub or GitLab, or in a
  project's `FOLLOWUPS.md`. sous keeps pointers and short notes, and works
  everything else out again each time you look.
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

Four groups, and each grows in one direction:

| Commands | Answers | Grows through |
|---|---|---|
| `sous`, `here`, `<project>`, `projects`, `report`, `review` | What is waiting on me? Where was I? What changed? What needs checking? | **signals** |
| `note`, `edit`, `kind`, `snooze`, `done`, `file` | Remember this, for that project | **backends** |
| `go` | Take me there | **launchers** |
| `go --run`, `show`, `reply` | Do this for me, and tell me how it went | **runners** |

Plain `sous` always shows the board. `here`, the session hooks and `go` stay
on your machine and finish quickly: they never wait on the network.

## Data

Local state lives in `~/.sous/` (or `SOUS_HOME`); filed notes live in their
selected backend. Each sous-owned JSON state file outside `runs/` has a
version number, is saved whole under a lock so a crash never leaves half a file, and is
upgraded when an older one is read. A file written by a newer sous is
refused, never overwritten.

The files, and what each holds, are listed in
[the command guide](commands.md#files-sous-keeps); a run's folder under
`runs/` is not versioned, since it belongs to its runner.

sous never writes rows of its own. Apart from your notes, everything is
worked out again on every full look.

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
