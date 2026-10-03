# sous command guide

Every command, every option, and what each one does. For the why and the
everyday flow, start with the [README](../README.md). Run
`sous <command> --help` for a one line reminder of any command.

* [How commands are written](#how-commands-are-written)
* [Seeing what is waiting](#seeing-what-is-waiting): `sous`, `sous <folder>`, `sous <project>`, `here`, `projects`, `report`
* [Notes](#notes): `note`, `show`, `edit`, `kind`, `snooze`, `done`
* [Sharing a note](#sharing-a-note): `file`, `note --file`, `done --close`
* [Going to a project](#going-to-a-project): `go`
* [Handing work to an agent](#handing-work-to-an-agent): `go --run`, `show`, `reply`, `done --clean`
* [Setting up](#setting-up): `setup`, `config`, `doctor`, `version`, `help`
* [Options for the shell and menu bar](#options-for-the-shell-and-menu-bar): `--ambient`, `--cached`, `--refresh`, `--menubar`
* [Commands for plugins and hooks](#commands-for-plugins-and-hooks): `signal`, `backend`, `launcher`, `runner`, `hook`
* [How project names are matched](#how-project-names-are-matched)
* [Exit codes](#exit-codes)
* [Environment variables](#environment-variables)
* [Configuration](#configuration)
* [Files sous keeps](#files-sous-keeps)

## How commands are written

* Options can go anywhere after the command, and can be written short or
  long: `-k me`, `--k me`, `-k=me`.
* `--` ends the options. Everything after it is text, even if it starts with
  a dash: `sous note -- "-2 tests failing"`.
* A command given an option it does not know, or the wrong number of
  arguments, stops with exit code `2` and prints its usage. Nothing is
  silently ignored.
* Every command that shows something takes **`--json`** for programs, and
  `here` also takes **`--brief`**. These two work anywhere on the line.
  Commands that change something do not take them, and say so.
* `sous <command> --help` (or `-h`) prints that command's usage.
* sous never stops to ask a question. It is safe to run from scripts and
  agents.

### What `--json` gives

Every list of things waiting is a list of the same **item**, whichever
command prints it, in sections named as the board names them: `on_you`,
`on_others`, `unfinished`, `ideas`, `snoozed` (waiting, but hidden until
the snooze ends), and `attention` (why the picture is incomplete; empty
when it is whole). An empty list is `[]`, never `null`. Times are UTC.

```json
{
  "id": "7",
  "project": "/home/sam/code/acme/billing",
  "name": "billing",
  "kind": "me",
  "text": "fix the flaky checkout test",
  "since": "2026-09-27T09:02:00Z",
  "source": "human",
  "ref": "github:acme/billing#12",
  "upstream": {"state": "open"},
  "run": {"runner": "claude", "state": "needs_you", "text": "which fixture?", "branch": "sous/run-7", "worktree": "/home/sam/.sous/runs/0a1b2c3d4e5f/worktree"},
  "snoozed": false,
  "stale": false,
  "closed_at": null,
  "closed_by": null
}
```

* `id`: a note's number (`"7"`, a string), or a signal's id (`"s:…"`).
  Either goes to `sous snooze`; a note's goes to `done`, `show` and the rest.
* `kind`: whose move it is: `me`, `them`, `idea` or `unfinished`. A run
  is `them` while it works and `me` once it is done, needs you, or failed.
* `text`: as written. The board's "run needs you · …" is for people;
  programs read `run`.
* `source`: who wrote a note (`human` or `agent`), or which plugin found it.
* `upstream`: for a filed note, what its tracker said: `open`, `closed`,
  `unknown` (gone there) or `error` (could not ask, with `error` saying
  why). `null` for a note that is not filed, and for signals.
* `run`: for a run, how it is going; `error` when its state could not be
  read this time (`state` is then the last one known). `null` otherwise.
* `stale`: its source failed this time, and this is what it said before.
* `closed_at`, `closed_by` (`you` or `upstream`): for a closed note.

## Seeing what is waiting

### `sous`

The board: everything waiting on you, across every project under your
roots.

```
sous · 2 on you · 1 on others · 3 unfinished
```

* **on you**: reviews asked of you, changes asked on your pull requests,
  merge requests to review, and notes you marked `me`.
* **on others**: your notes marked `them`, and your merge requests waiting
  for review.
* **unfinished**: work left in git: uncommitted files, unpushed commits,
  stashes, branches that were never pushed. Oldest first.

Every row has an id (a note number like `3`, or a row id like
`s:0b4ac59dfd33`) and an age: how long it has been waiting.

The headline shows `?` with the reason whenever the picture is incomplete:
a source failed, a folder is missing, or some rows are out of date and
marked `(stale)`. It never shows a calm zero when data is missing.

Ideas and snoozed rows are not on the board. They show in `sous here`.

Options: `--json`: every section, ideas and snoozed included (see
[What `--json` gives](#what---json-gives)), with `configured`, `projects`,
`plugins` (how each source did), `checked`, `unavailable` and `as_of`.

On a first run with no roots set, `sous` prints a short welcome that says
how to set them.

### `sous <folder>`

The board for just the projects in that folder, for example `sous ~/code/acme`.
It does not update the saved board the shell and menu bar use.

### `sous <project>`

Where you left off in a project, found by name from anywhere. The same as
`sous here` inside that project. See
[how names are matched](#how-project-names-are-matched).

### `sous here [path]`

Where you left off in the project you are in, or the one at `path`:

* the branch, the last commit and its message
* how your last agent session there ended (Claude Code, and Codex when
  `sous setup --codex-session-end` asked it to report session ends)
* what is on you and on others in this project, including snoozed rows
* your ideas for this project
* notes that were closed in their tracker lately, marked
  `✓ ... (closed upstream)`
* next step hints

A filed note shows its ref (`→ github:acme/api#12`). `(ref missing)` means
the tracker no longer has the item, and `(status unavailable: ...)` means
sous could not ask. Neither is taken to mean done.

`here` is fast and never waits on the network. It checks git itself and
reads what the board learned last about GitHub and GitLab.

Options:

* `--brief`: a shorter version, what agents see when a session starts:
  five rows of each kind with where the rest are, and every note and the
  last message cut to one line, ending in `…` when cut.
* `--json`: the sections, with `facts` (branch, last commit), `session`
  (how the last agent session here ended, or `null`), `recently_closed`,
  and `plugins`.

### `sous projects [name]`

Every project sous can see: org, name, host and the age of the last commit.
Give a name to filter the list; a name that matches nothing says so and
exits `0` (`[]` with `--json`).

Options:

* `--root <folder>`: look in this folder instead of your configured roots.
  Can be given more than once.
* `--path <name>`: print just the full path of the one project that
  matches. Useful in scripts: `cd "$(sous projects --path api)"`.
* `--json`

### `sous report`

What changed since your last report: new things on you and on others, new
ideas, what got closed (by you or in a tracker), which projects you worked
in, and what needs a look. The first report looks back one day.

Options:

* `--week`: the last seven days, whatever you saw before. Does not move the
  "since your last report" mark.
* `--open`: write the report as a page (`~/.sous/report.html`) and open it
  in your browser. The page has no scripts and loads nothing from the
  internet.
* `--json`: `new_on_you`, `new_on_others`, `new_ideas` and `closed` as
  items, `worked`, `attention`, and `now` (how many are waiting at the end
  of the window).

A report only counts as seen when it was shown. If writing or opening it
fails, the next report still covers the same time.

## Notes

Notes are private to you. They live in `~/.sous/threads.json` and nowhere
else until you [share one](#sharing-a-note).

### `sous note "text"`

Save a note, and say its number and what makes sense next:
`noted 7 in api · sous done 7`. Saving the same text, kind
and project again while that note is open gives back the same note
(`already noted as 7`), so a retry never makes two. With `--json`:
`{"id": "7", "did": "noted", "ref": null, "next": [...]}`; every command
that changes something answers this way, and `did` says what happened
(`noted`, `already_noted`, `edited`, `kind`, `snoozed`, `closed`,
`already_closed`, `filed`, `replied`, and for `go --run` `started` and
`already_started`).

Options:

* `-p <project>`: the project it is for. Leave it out to use the project
  you are in. The name can be rough; see
  [how names are matched](#how-project-names-are-matched).
* `-k me|them|idea`: whose move it is. `me`: you have to do it, shown under
  "on you". `them`: you are waiting on someone, shown under "on others".
  `idea` (the default): kept for when you next open the project, not shown
  on the board.
* `--file`: also send it to the project's tracker. See
  [sharing a note](#sharing-a-note).

When an agent writes a note on its own, it sets `SOUS_SOURCE=agent` so you
can tell its notes from yours.

### `sous show <n>`

Shows one note in full: its project, text, kind and age, where it was
filed, and the commands that make sense next. For a run, it asks the
runner how it is going right then (see
[Handing work to an agent](#handing-work-to-an-agent)). A closed note can
still be shown.

Options: `--json`: the note as an item, with `uid` (its id that never
changes), `remote`, `snoozed_until`, and `next`, the commands that make
sense next.

### `sous edit <n> "text"`

Change the text of note `n`. The text is taken as is, dashes and all.

### `sous kind <n> me|them|idea`

Change whose move note `n` is.

### `sous snooze <n> [days]`

Hide note `n` for a number of days (seven if you leave it out). It still
shows in `sous here`, marked snoozed.

### `sous snooze <s:id>`

Hide a git, GitHub or GitLab row until it changes. For example, a snoozed
"3 files uncommitted" comes back when you edit those files again. You can
type just the start of the id (`s:0a70`) as long as only one row matches.
Days do not apply to these rows.

### `sous done <n>`

Close note `n`. It only closes your note; a filed item stays open in its
tracker. Use `--close` to close both. Closing a note that is already
closed says when it was, and exits `0`, so a retry is safe.

For a run, `done` also stops the agent if it is still working. Its branch
and worktree stay, for you to look at or merge.

* `--clean`: for a run, also remove its worktree. Its branch stays when it
  has commits, and a worktree with changes not yet committed is never
  removed: `done` says so and leaves the note open, so no work is lost.

## Sharing a note

Filing puts a note where the project already keeps its work, so others can
see it. It only happens when you ask.

sous picks the tracker in this order: the project's `FOLLOWUPS.md` if it
has one, then GitHub issues if the project is on GitHub, then GitLab issues
if it is on GitLab. You can choose one per project in
[configuration](#configuration) with `backend`.

Filed items carry a small hidden marker (`<!-- sous:0123456789ab -->`) so
sous can find them again. Do not edit it. Filing the same note twice never
creates a second item.

From then on, every board checks the item. When it is closed in its tracker
(the box is ticked, or the issue is closed), your note closes too, marked
closed upstream.

### `sous file <n>`

File note `n` and print its ref, such as `github:acme/api#12` or
`md:FOLLOWUPS.md:0123456789ab`.

Options:

* `--force`: file even from a git worktree or submodule. Without it, sous
  refuses, so markers do not land on a branch that may be thrown away.

### `sous note --file "text"`

Save and file in one step. If filing fails, the note is still saved, and
sous says so.

### `sous done <n> --close`

Close the item in its tracker, then close your note. If the tracker could
not close it, your note stays open.

## Going to a project

### `sous go <project>`

Start your agent in that project, with a short summary of where you left
off. The summary is also saved to a file named by `SOUS_HERE_FILE`, for
agents that want to read it. `sous go .` means the project you are in.

In zsh, with the snippet from `sous setup`, your terminal stays in the
project after the agent exits. (bash and fish show the board in new
terminals, but have no `sous go` wrapper yet.)

Options:

* `-a <agent>` (or `--agent`): which agent to start: `claude`, `codex`, or
  the name of a launcher plugin. The default is `agent` in configuration,
  or `claude`.
* `--where`: only print the project folder `go` would use, and start
  nothing. The shell snippet uses it to move your terminal there.
* `--in <folder>`: use this folder instead of looking the project up. The
  shell snippet passes the folder it already found.
* `--run <brief>`: hand a task to an agent in the background instead, and
  return at once. See [Handing work to an agent](#handing-work-to-an-agent).
* `--key <text>`: with `--run`, a name for the task. Running `go --run`
  again with the same key gives back the run already started. Without a
  key the brief is the key, so a retry of the same brief never starts it
  twice; a key is only needed to start the same brief twice on purpose.

## Handing work to an agent

### `sous go <project> --run`

You, or your agent, can hand a task to an agent that works on it in the
background. The run shows on the board: waiting on others while it works,
then on you when it is done, needs an answer, or failed. The context your
agent gets when a session starts lists the runs that need you first.

```
sous go billing --run - --json <<'EOF'
Fix the flaky test in checkout_spec. It fails about 1 in 5 runs on CI
since the fixtures change. Done means it passes 20 times in a row.
EOF
```

`--run -` reads the brief from standard input; write what to do, why, and
what done means. `-a` picks the runner (`claude`, `codex`, or a runner
plugin); the default is `runner` in configuration, or `agent` when that is
not set. The answer names the
run's number, and the command to check on it:

```json
{"id": "7", "did": "started", "ref": null, "run": {"runner": "claude", "state": "running", ...}, "next": ["sous show 7", "sous done 7"]}
```

The built in runners work in a new git worktree, on a branch named
`sous/run-7`, so your own checkout is never touched. They never push: the
branch waits for you. An agent that could not commit leaves its changes in
the worktree, and the run's status says "changes not committed". The agent gets the permissions you already gave it,
plus what committing on its own branch needs: Claude accepts file edits,
may run `git add`, `git commit`, `git status`, `git diff` and `git log`,
and otherwise follows your Claude settings; Codex works in its workspace
sandbox, which may also write git's objects, refs and logs, but never your
repo's hooks or settings. For anything more, the agent asks you. A run may
take `run_minutes` (60 by default) before it is stopped. A run going when
your computer restarts shows as failed, "stopped without a result": the
built in runners do one thing, and never resume. For work that must
survive that, use a runner plugin.

A run is a note, so `snooze`, `edit` and `done` work on it.

### `sous show <n>` for a run

Shows how the run is going: `running`, `needs you` with its question,
`done` with its summary, or `failed` with the reason, and where its branch,
worktree and log are.

### `sous reply <n> "answer"`

Answers a run that needs you. The agent carries on in the same session. A
run that is still working cannot be answered yet, and one that never
started a session cannot carry on: start a new run with the answer in its
brief.

### `sous done <n> --clean` for a run

Stops the run if it is still working, closes it, and removes its worktree.
Its branch stays when it has commits. A worktree with changes not yet
committed is kept, and the note stays open, until you commit or copy them.

## Setting up

### `sous setup [folder...]`

Sets everything up, and is safe to run again at any time. It tells you what
it did.

* **Projects.** Folders you name become your roots. With none named, it
  keeps the roots you have, or looks in the usual places (`~/code`, `~/src`,
  `~/dev`, `~/projects`, `~/workspace`, `~/repos`, `~/git`, `~/Developer`,
  `~/Projects`, `~/Documents/GitHub`) and uses those that hold git repos.
* **Agents.** Adds session hooks for Claude Code (session start and end) and
  Codex (session start). Puts the `sous` skill where agents look for skills:
  Claude Code's and Codex's folders, the shared `~/.agents/skills` folder
  that Gemini CLI, Kimi, Cursor and others read, and Antigravity's folder
  when Antigravity is installed. A sous that moved updates its hooks rather
  than adding a second set.
* **Shell.** Adds one line to your shell's startup file so new terminals
  show the board: `~/.zshrc` for zsh (or the one in `$ZDOTDIR`),
  `~/.bashrc` for bash, and on macOS also the login file bash reads there
  (`~/.bash_profile`, `~/.bash_login` or `~/.profile`, whichever you have),
  and `~/.config/fish/conf.d/sous.fish` for fish. An older sous line is
  updated in place; nothing is ever added twice.
* **Menu bar.** Writes `~/.sous/sous.5m.sh` for
  [SwiftBar](https://swiftbar.app), pointed at this copy of sous.

Options:

* `--no-shell`: leave your shell's startup file alone.
* `--print-skill`: only print the `sous` skill, to add to another agent.
  Changes nothing.
* `--codex-session-end`: also add a Codex session end hook, for Codex
  versions that support it.

### `sous config`

Shows every setting, its value, and whether it is a default, then any
settings for particular projects.

    sous config                                   show everything
    sous config agent codex                       change a setting
    sous config ignore 'scratch/*' 'tmp/*'        a list takes several values (this replaces it)
    sous config plugins --add ~/bin/sous-signal-todo   add to a list, keeping what is there
    sous config ignore --remove 'tmp/*'           take something out of a list
    sous config -p api backend markdown           one project (rough name)
    sous config -p 'acme/*' github_account work   every project in the acme folder
    sous config --unset agent                     back to the default
    sous config -p api --unset backend
    sous config -p api                            one project's own settings

The settings are described in [Configuration](#configuration). Values are
checked before anything is written: an agent, runner or backend must be
one sous knows, and `refresh_hours` a whole number above 0. A per-project setting
sous does not read itself is written anyway (a plugin may read it), with a
note, so a typo does not pass unnoticed.

The file is edited the way a person would: comments and layout stay, a
removed setting takes its comment with it, a table left empty goes too, and
the file keeps its own line endings. sous refuses, and changes nothing, if
a setting is written in a way it cannot change safely, or if a list holds
comments that a new value would lose.

Options:

* `-p <project>`: work on one project's settings. The name can be rough, or
  `org/*` for every project in an org folder.
* `--unset`: remove the setting. `roots` cannot be removed; change it
  instead.
* `--add`, `--remove`: add to or take from a list setting (roots, ignore,
  plugins, gitlab_hosts) instead of replacing it.
* `--json`

### `sous doctor`

Checks that sous is set up and working, and for anything that is not, says
the command that fixes it. It only looks: it changes none of your files or
settings. It checks:

* config.toml and your project folders (and how many projects are in them)
* the agent hooks for Claude Code and Codex, and that they run this sous
* the sous skill in every folder agents read, and that it is current
* the line in your shell's startup file, and that `sous` is on your PATH
* `gh` and `glab`: installed, logged in, each configured account or host,
  and whether each GitHub login can read notifications
* each plugin you listed, and that its name is one sous will use
* the agent each built in runner starts (`claude`, `codex`): a missing one
  is worth a look, since `go --run` cannot use it
* your notes, sessions and observations files
* how old the saved board is, and each source its last refresh could not
  read, with the reason (this is what a `?` on the board means)

```
sous doctor · 16 checks · 0 problems · 1 to look at

  ✓ config: ~/.sous/config.toml reads
  ✓ project folders: 26 projects in ~/code
  ...
  ! shell (zsh): new shells do not show the board (~/.zshrc has no sous line)
      fix: sous setup
```

`✗` is broken: the board is missing something because of it. `!` is worth a
look but may be your choice, such as no shell line after
`sous setup --no-shell`, or a tracker you never set up. It exits `1` when
something is broken, else `0`. With `--json` it prints `problems`,
`warnings` and every check with its `name`, `status` (`ok`, `warn` or
`bad`), `detail` and `fix`.

This is the one command that asks GitHub and GitLab directly, so it can
take a few seconds. A `gh` or `glab` that has not answered in 30 seconds is
reported as broken.

Options: `--json`.

### `sous version`

Print the version. Builds from source show how far past a release they are,
like `v0.1.0-5-g21e7aa8`.

### `sous help`

A short list of every command. `sous --help` does the same.

## Options for the shell and menu bar

These go first on the line, on their own.

| Option | What it does |
|---|---|
| `sous --ambient` | What new shells run. Prints the saved board, at most once per `refresh_hours`. |
| `sous --cached` | Prints the saved board right away, with its age. If it is older than `refresh_hours`, a fresh one is built in the background for next time. With no saved board yet, it starts one and exits `3`. |
| `sous --refresh` | Builds a fresh board and saves it, printing nothing. |
| `sous --menubar` | Prints the saved board in SwiftBar's format. Never builds one. |

## Commands for plugins and hooks

You rarely need these by hand. They are how sous runs its own built in
parts, the same way it runs plugins, and they are handy for testing a
plugin. See [plugins.md](plugins.md).

| Command | What it does |
|---|---|
| `sous signal <name> scan` | Run a built in signal (`git`, `github`, `gitlab`). Project folders on standard input, one JSON line per finding out. |
| `sous backend <name> <call> ...` | Run a built in backend (`markdown`, `github`, `gitlab`): `detect`, `file`, `status`, `close` or `url`. |
| `sous launcher <name> run <folder>` | Start a built in launcher (`claude`, `codex`) in a folder. |
| `sous runner <name> <call> ...` | Run a built in runner (`claude`, `codex`): `start`, `status`, `reply`, `stop` or `clean`. |
| `sous hook session-start\|session-end <agent>` | What the agent hooks run. Always exits `0` and never holds up a session. |

## How project names are matched

Wherever a command takes a project name (`-p`, `sous <project>`, `go`,
`projects --path`), sous tries these in order, ignoring case, and uses the
first step that finds anything:

1. the exact name: `api`
2. the exact org and name: `acme/api`
3. the start of a name: `ap`
4. the start of an org and name: `acme/a`
5. any part of the org and name: `me/ap` finds `acme/api`
6. the letters of the org and name in order: `aapi` finds `acme/api`

If a step finds more than one project, sous lists them and stops with exit
code `2`. It never guesses.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Fine. |
| `1` | Something failed: a data file, a tracker, a missing tool. The message says what. For `sous doctor`, something is broken. |
| `2` | The command was wrong (unknown option, wrong arguments, bad value), or a name matched more than one project. |
| `3` | `--cached` or `--ambient` was asked for the board before there was one. A first board is being built. For `sous go`, the agent it starts is not installed; for `go --run`, the runner is not set up, and the run shows on the board as failed. (For a signal or runner plugin, exit `3` means "not set up here"; see [plugins.md](plugins.md).) |

## Environment variables

| Variable | What it does |
|---|---|
| `SOUS_HOME` | Where sous keeps its files. Default `~/.sous`. |
| `SOUS_SOURCE` | Who is writing a note. Agents set `agent`; the default is `human`. |
| `SOUS_HERE_FILE` | Set by `sous go` for the agent it starts: a file with where you left off. |
| `SOUS_BIN` | For the install script and the menu bar script: where `sous` lives. |
| `SOUS_VERSION` | For the install script: which release to install, like `v0.1.4`. |

## Configuration

`~/.sous/config.toml`. Every setting is optional except `roots`, which
`sous setup` writes for you. Change settings with
[`sous config`](#sous-config), or edit the file by hand.

```toml
roots = ["~/code", "~/work"]   # where your projects live; sous looks two folders deep
ignore = ["scratch/*"]         # folders to skip, as org/name patterns
agent = "claude"               # the agent sous go starts
runner = "codex"               # the runner sous go --run hands work to; agent when not set
refresh_hours = 4              # how old the saved board may get, and how often new shells print it
run_minutes = 60               # how long a run by a built in runner may take
gitlab_hosts = ["git.example.org"]   # GitLab servers, beyond the ones glab knows
plugins = ["~/.sous/plugins/sous-backend-jira"]   # plugins to run; only listed ones run

[projects."acme/*"]            # every project in the acme folder
github_account = "work-account"   # which gh account to use for these projects

[projects."acme/billing"]      # one project; an exact name wins over org/*
backend = "markdown"           # always file to FOLLOWUPS.md, creating it if needed
```

| Setting | Default | What it does |
|---|---|---|
| `roots` | none | Folders to look for projects in, two folders deep. A root that is itself a repo counts. |
| `ignore` | none | Patterns like `scratch/*` or `*/tmp`, matched against `org/name`. |
| `agent` | `claude` | The agent `sous go` starts when `-a` is not given. |
| `runner` | `agent` | The runner `sous go --run` hands work to when `-a` is not given: `claude`, `codex`, or a runner plugin's name. |
| `refresh_hours` | `4` | How old the saved board may get, and how often new shells print it. |
| `run_minutes` | `60` | How long a run by a built in runner may take before it is stopped. |
| `gitlab_hosts` | none | GitLab servers to use, beyond the ones `glab` is logged in to. |
| `plugins` | none | Paths to plugin programs. Only listed plugins run. |
| `[projects."<org>/<name>"]` or `[projects."<org>/*"]` | | Settings for one project, or every project in an org folder. |
| `backend` | picked for you | Where `sous file` sends this project's notes: `markdown`, `github`, `gitlab`, or a plugin's name. |
| `github_account` | `gh`'s active account | Which `gh` account to use for these projects, when you have more than one. |

## Files sous keeps

Everything lives in `~/.sous` (or `SOUS_HOME`).

| File | What is in it |
|---|---|
| `config.toml` | your settings |
| `threads.json` | your notes |
| `observed.json` | what sous has seen before: when each row first appeared, and what you snoozed |
| `sessions.json` | the last agent session in each project |
| `cache.json` | the saved board, for new shells and the menu bar |
| `report.json` | when you last saw a report, where the next one starts |
| `install-id` | a random id for this install, used in older filing markers. Do not delete it. |
| `here/` | the files `sous go` hands to agents, one per project |
| `runs/` | one folder per run by a built in runner: its worktree, log and result |
| `report.html` | the last report page |
| `sous.zsh`, `sous.5m.sh` | the zsh snippet and the menu bar script |
| `.ambient-stamp` | when a new shell last printed the board |
| `*.lock` | short lived locks so several sous processes can write safely |

sous also keeps one lock file, `sous/gh-token.lock`, in your cache folder
(`~/Library/Caches` on macOS, `~/.cache` on Linux).

Each data file carries a version and is upgraded when a newer sous reads it.
An older sous refuses a newer file rather than damaging it.
