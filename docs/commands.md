# sous command guide

Every command, every option, and what each one does. For the why and the
everyday flow, start with the [README](../README.md). Run
`sous <command> --help` for a one line reminder of any command.

* [How commands are written](#how-commands-are-written)
* [Seeing what is waiting](#seeing-what-is-waiting): `sous`, `sous <folder>`, `sous <project>`, `here`, `projects`, `report`
* [Notes](#notes): `note`, `edit`, `kind`, `snooze`, `done`
* [Sharing a note](#sharing-a-note): `file`, `note --file`, `done --close`
* [Going to a project](#going-to-a-project): `go`
* [Setting up](#setting-up): `setup`, `version`, `help`
* [Options for the shell and menu bar](#options-for-the-shell-and-menu-bar): `--ambient`, `--cached`, `--refresh`, `--menubar`
* [Commands for plugins and hooks](#commands-for-plugins-and-hooks): `signal`, `backend`, `launcher`, `hook`
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

Options: `--json`.

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
* how your last agent session there ended (Claude Code; Codex does not report
  session ends yet)
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

* `--brief`: a shorter version (the last message is cut to 300 characters
  and only five ideas are listed). This is what agents see when a session
  starts.
* `--json`

### `sous projects [name]`

Every project sous can see: org, name, host and the age of the last commit.
Give a name to filter the list.

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
* `--json`

A report only counts as seen when it was shown. If writing or opening it
fails, the next report still covers the same time.

## Notes

Notes are private to you. They live in `~/.sous/threads.json` and nowhere
else until you [share one](#sharing-a-note).

### `sous note "text"`

Save a note and print its number.

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
tracker. Use `--close` to close both.

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

With the shell snippet from `sous setup`, your terminal stays in the
project after the agent exits.

Options:

* `-a <agent>` (or `--agent`): which agent to start: `claude`, `codex`, or
  the name of a launcher plugin. The default is `agent` in configuration,
  or `claude`.
* `--where`: only print the project folder `go` would use, and start
  nothing. The shell snippet uses it to move your terminal there.

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
* **Shell.** Adds one line to your zsh, bash or fish startup file so new
  terminals show the board. It is only added once.
* **Menu bar.** Writes `~/.sous/sous.5m.sh` for
  [SwiftBar](https://swiftbar.app), pointed at this copy of sous.

Options:

* `--no-shell`: leave your shell's startup file alone.
* `--print-skill`: only print the `sous` skill, to add to another agent.
  Changes nothing.
* `--codex-session-end`: also add a Codex session end hook, for Codex
  versions that support it.

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
| `1` | Something failed: a data file, a tracker, a missing tool. The message says what. |
| `2` | The command was wrong (unknown option, wrong arguments, bad value), or a name matched more than one project. |
| `3` | `--cached` or `--ambient` was asked for the board before there was one. A first board is being built. (For a signal plugin, exit `3` means "not set up here"; see [plugins.md](plugins.md).) |

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
`sous setup` writes for you.

```toml
roots = ["~/code", "~/work"]   # where your projects live; sous looks two folders deep
ignore = ["scratch/*"]         # folders to skip, as org/name patterns
agent = "claude"               # the agent sous go starts
refresh_hours = 4              # how old the saved board may get, and how often new shells print it
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
| `refresh_hours` | `4` | How old the saved board may get, and how often new shells print it. |
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
| `install-id` | a random id for this install, used in older filing markers. Do not delete it. |
| `here/` | the files `sous go` hands to agents, one per project |
| `report.html` | the last report page |
| `sous.zsh`, `sous.5m.sh` | the zsh snippet and the menu bar script |

Each data file carries a version and is upgraded when a newer sous reads it.
An older sous refuses a newer file rather than damaging it.
