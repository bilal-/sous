# Changelog

Every change people will notice is listed here, newest first. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Before 1.0,
commands and flags may change between minor versions; data files are always
upgraded, never broken.

## [Unreleased]

### Fixed
* `sous done <n> --close` (or `--clean`) after a plain `done` closes the
  filed item (or removes the run's worktree) again, as it did before
  0.7.0; the answer still says the note was already closed.
* `note --file --json` that saves the note but cannot file it prints one
  JSON answer, with the note's id and why it was not filed, instead of
  two.
* Retrying `go --run` while its runner is not set up starts the same note
  again instead of leaving a failed note for every try.
* A runner that is not set up says "not set up" once, not twice. A `gh`
  or `glab` that cannot answer (the network is down) is reported as an
  error, not as "not set up".

### Changed
* `report --json` only reads: the next report still starts where the last
  one you saw ended, so a program can ask as often as it likes. Before,
  reading it used up the report for you.
* `note` with the same text as an open note in the same project is that
  note whatever kind it was given, and says which kind it has and how to
  change it. A note written over several lines says it was joined onto
  one.
* A failed run suggests trying again under a new key
  (`sous go <project> --run - --key retry-<n>`), as well as `show` and
  `done --clean`.
* `done --clean` on a run already closed says it cleaned it up
  (`did: cleaned`). `kind` and `snooze` answers carry the new kind and
  when the snooze ends.
* With `--json`, a name that matches several projects lists them under
  `matches`.
* `here` marks ideas `(idea)`, as it marks others' rows `(them)`; its short
  form lists snoozed rows last; the board says when it cut a note. In
  every `--json`, what is snoozed is under `snoozed`.
* `sous help <command>` is that command's help. A mistyped command says
  which one it most likely meant. Errors say "note" where they said
  "thread".
* `go --run --json` answers like every other change: `did` is `started`
  or `already_started`, and the run is under `run`. When the run it finds
  already failed or finished, the text says so.

## [0.7.0]

### Changed
* Every command that changes something says what it did and what makes
  sense next, in one line (`noted 7 in api · sous show 7 · sous done 7`,
  `closed 7 · sous`), and takes `--json` for the same answer as data:
  `{"id", "did", "ref", "next"}`. `note` used to print only the number,
  and `edit`, `kind`, `snooze` and `done` printed nothing.
* Retries are safe. `done` on a note already closed says when it was
  closed and exits `0`. `note` with the same text, kind and project as an
  open note gives back that note. `go --run` without `--key` uses the
  brief as its key, so the same brief retried finds the run already
  started.
* `--brief` is refused by commands other than `here`, as `--json` is by
  commands that neither show nor change anything.
* When a note cannot be filed because the project has no tracker, the
  error says what would make it fileable.
* With `--json`, a command that fails also writes the error to standard
  output as `{"error", "exit"}`, so a program reading it is never left
  with nothing.
* `sous projects <name>` that matches nothing says `0 projects match` and
  exits `0`; `--path` still needs exactly one.
* A word that is neither a command nor a project (`sous lsit`) says so,
  instead of looking for a project by that name.
* `sous --version` works.
* What an agent hears at session start stays short: five rows of each
  kind, then `… and N more (sous <project>)`, and a long note cut to 120
  characters. Started outside any project, it now hears the board in one
  line instead of nothing. It is told to run the `sous` it can reach: the
  one on PATH, or this one by its full path.
* Text cut short ends in `…`, on the board and in `here`, and `here` says
  `sous show <n>` gives the whole note. The board used to cut silently.
* `sous show` on a failed run shows the last lines of its log, and
  `--json` has them as `log_tail`.
* Each command's `--help` now says everything docs/commands.md says about
  it, not only its usage line.
* Every failed check in `sous doctor` names a command to run.
* An empty board's `--json` has `"as_of": null`, not the year 1.

## [0.6.0]

### Changed
* A run reads the same everywhere: the board, `sous show`, `--json` and
  what an agent hears at session start use one wording and one list of
  next commands. A failed run now suggests `sous show <n>` before
  `sous done <n> --clean`.
* Exit code `3` means "not set up" for every plugin call and for
  `sous go`: a built in launcher whose agent is missing, `sous go` with a
  missing agent, and a built in backend whose `gh` or `glab` is logged out
  for a project that would be its own. They exited `1` before.
* `sous kind` on a run says no: a run is on others while it works and on
  you once it stops.
* In `sous --json`, a snoozed idea is listed under `snoozed`, like every
  other snoozed note. A run's item also has `log`, `key` and `checked_at`.
* A backend's `detect` now hears the reason when sous cannot reach a
  tracker that would own the project: `sous file` says "not set up" with
  what is missing, instead of quietly keeping the note local.
* Inside sous, a review round removed every duplicated piece of logic
  the clone detector finds: one way to list each kind of plugin
  (`Registry.All`, `Registry.Offline`), one table for how a run reads, one
  set of exit codes, one place for request decoding and answers, for
  text shaping, and for the hook command. AGENTS.md describes each.

### Fixed
* `sous show` called a run that waits on you "them"; it says "me", as the
  board and `--json` do.
* A snoozed run is no longer announced at session start.

## [0.5.1]

### Fixed
* 0.5.0 was tagged but never released: a test left a background run
  writing to its folder after it ended, and the release build failed on
  it. 0.5.1 is 0.5.0 with that test fixed; nothing else changed.

## [0.5.0]

### Changed
* `--json` has one shape for anything waiting, an **item**, whichever
  command shows it, and the board, `here` and `report` list items in the
  sections the board uses (`on_you`, `on_others`, `unfinished`, `ideas`,
  `snoozed`) with `attention` saying what is missing. docs/commands.md
  describes it once. What changed for a program reading it:
  * `sous --json` gives sections instead of raw `threads` and `signals`,
    and now includes ideas and snoozed notes. `rendered_at` is `as_of`.
  * `here --json` gives sections too; `session` has `id` and `ended_at`.
  * `report --json`: `new_me` and `new_them` are `new_on_you` and
    `new_on_others`; the counts are under `now`; times are UTC.
  * A note's `id` is a string everywhere (`"7"`), as signal ids are.
  * A run's text is the note as written; its state is under `run`
    (`run.state`, not `run_state`). Upstream state is under `upstream`.
  * `closed` (a time) is `closed_at` in `show --json`.
* Inside sous: `cli` decides nothing about the data any more (closing a
  run, the next commands for a note and how a note reads moved to `runs`
  and `board`); ages, times and cut lines live in `internal/text`; notes
  are listed through one filter.

### Fixed
* Since 0.4.0's test setup, part of the command line tests stopped running
  without saying so: one test replaced the test process with a fake agent.
  No test can do that now, and every test runs again. 0.4.0 passes them
  all.
* The context an agent gets at session start no longer runs git for each
  run it lists.

## [0.4.0]

### Added
* A `runner` setting: the runner `sous go --run` hands work to when `-a`
  is not given. A runner plugin can now be the default. When it is not
  set, `agent` picks the runner, as before.


### Changed
* Inside sous, every agent tool it works with is one file in
  `internal/harness`: its skills, hooks and their file layout, what its
  hooks send, its transcript, and how to run it headless. setup, doctor,
  the hooks, `sous go` and runs all read that table (see AGENTS.md,
  Adding an agent).
* Inside sous, what refs, links, ssh remotes and `sous doctor` know about
  GitHub and GitLab is one table, `tracker.Kinds`. AGENTS.md has the real
  recipe for adding a tracker.

### Fixed
* With `sous setup --codex-session-end`, a Codex session that ends now
  records its last message, so `sous here` says where it stopped. sous read
  Codex's transcript as if Claude Code had written it and found nothing.
* A session hook that is sent nothing, or something it cannot read, now
  treats it as a new session in the folder the hook runs in, instead of
  doing nothing.


## [0.3.3]

### Changed
* Every kind of plugin speaks the same exit codes for every call: `0`
  done, `1` failed, `2` refused, `3` not set up. The plugin guide now has
  one table for them. A backend's `detect` may exit `3` to say it would
  handle a project but is not set up here; sous shows the reason and asks
  the next backend.
* Inside sous, each kind of plugin keeps its built ins in one list that
  discovery, `sous <kind> <name> <call>` and the session start all read.
  A built in is one entry there (see AGENTS.md).

### Fixed
* A signal plugin's error message is cut at 200 characters without
  splitting a character in two.

## [0.3.2]

### Fixed
* A backend whose `status` exits 2 or prints something other than `open`,
  `closed` or `unknown` is now read as "status unavailable". sous used to
  take it as `unknown` and could file the note a second time.
* A runner whose `stop` exits 2 is no longer taken as stopped, so
  `sous done` keeps the note open while the run goes on.
* When a plugin refuses a call with exit 2, the reason it printed on
  standard error now reaches you.
* `--json` never shows an empty list as `null`; it is `[]`. This was wrong
  in the board, `here` and `report`.
* In `report --json`, a run that is done, needs you or failed says
  `"kind": "me"`, as the section it is listed in does.
* When no project matches a name and a project folder could not be read,
  the error says so: the project may be in that folder.
* `sous help` lists both meanings of exit code 3.
* A build from source without a version says `sous dev`, not `0.1.0-dev`.

### Changed
* `sous go --run --json` gives `next` as a list, as `sous show --json`
  does.

## [0.3.1]

### Changed
* The sous skill now tells agents to reach for sous when you want a task
  handed to an agent in the background. Run `sous setup` to update it;
  `sous doctor` shows it as out of date until then.

## [0.3.0]

### Added
* Runs: hand a task to an agent that works on it in the background, and
  follow it on the board. `sous go <project> --run <brief>` (or `--run -`
  for a brief on standard input) starts one; `--key` makes a retry safe.
  The board shows it on others while it works, then on you when it is
  done, needs you, or failed. The context your agent gets at session start
  lists runs that need you first.
* `sous show <n>`: one note in full; for a run, how it is going now.
* `sous reply <n> "answer"`: answer a run that needs you; it carries on.
* `sous done <n> --clean`: for a run, also remove its worktree.
* The built in `claude` and `codex` runners: each starts one agent in a new
  git worktree on a branch `sous/run-<n>`, with the permissions you already
  gave it plus what committing on that branch needs, never pushes, and
  stops after `run_minutes` (60 by default).
* The runner contract, a fourth kind of plugin (`sous-runner-<name>`), and
  `runnertest`, its conformance suite.
* `sous doctor` checks the agent each built in runner needs.

### Changed
* sous is no longer described as never running agents. It hands work to
  runners and shows what they report; the built in runners start one agent
  and nothing more.

## [0.2.3]

### Fixed
* With `glab` installed but never logged in, sous took gitlab.com as a
  host you use, so every board showed GitLab as failed and `sous doctor`
  called it broken. glab lists gitlab.com with "No token found"; sous now
  reads that as not set up.

### Added
* Every release is installed in a clean home on macOS and Linux, with zsh
  and bash, and must pass `sous doctor` there.

## [0.2.2]

### Added
* `sous doctor` checks every part of the setup (config and project
  folders, agent hooks and skills, the shell line, `gh` and `glab` logins
  and accounts, each login's notification access, plugins, data files, the
  saved board, and why the last refresh could not read a source) and says
  the command that fixes anything that is not right. It exits 1 when
  something is broken. `--json` for agents.

## [0.2.1]

### Fixed
* `sous config` edits the file the way a person would: removing a setting
  takes its trailing comment with it, removing a project's last setting
  removes its empty table, and files with Windows line endings keep them.
* Replacing a list that holds comments is refused instead of silently
  dropping the comments.
* A per-project setting sous does not read itself is written with a note,
  so a typo such as `backnd` does not pass unnoticed.

## [0.2.0]

### Added
* Failing checks on your open pull requests are on you: "checks failing ·
  PR #14". A new push ends a snooze on the row.
* Unread GitHub notifications addressed to you are on you: mentions, team
  mentions and assignments on issues and pull requests. Reading the
  notification on GitHub clears the row. Notifications need a classic
  token; if yours cannot read them, sous says how to fix it
  (`gh auth refresh -s notifications`).
* `sous config` shows every setting and changes them, for everything or
  for one project (`-p api`, or `-p 'acme/*'`), checking values first and
  keeping the file's comments and layout. `--add` and `--remove` change a
  list without replacing it.
* The plugin contract is stable: version 0 only grows until 1.0, which
  freezes it. docs/plugins.md says what that promises.
* An example plugin, `examples/sous-signal-todo`, in plain shell, and a
  conformance suite for signal plugins (`signaltest`) alongside the one
  for backends.

## [0.1.7]

### Fixed
* Ticking a box in a FOLLOWUPS.md with Windows line endings ticks the right
  box. Before, with many items above it, it could tick the wrong one.
* Two sous processes reading an old data file at once can no longer lose
  a note that was saved in between.
* Remotes like `host:org/repo` keep their org again. SSH host aliases are
  read from `~/.ssh/config` without running `ssh`, so nothing runs and
  nothing reaches the network.
* A note filed just before upgrading to 0.1.1 and retried after is not
  filed twice.
* GitLab that is logged out or missing is "not set up" (earlier rows are
  kept, marked stale), not a quiet empty result that drops them.
* Hooks name the stable `sous` on your PATH, so `brew upgrade` no longer
  breaks them, and only real sous hooks are ever replaced.
* bash startup lines only run in interactive shells (so `scp` and `rsync`
  keep working), macOS bash gets `.bash_profile` too, zsh respects
  `ZDOTDIR`, and symlinked startup files stay links.
* `sous setup` keeps multi-line or indented `roots` intact and never
  writes a config.toml that does not parse.
* The session end hook has the same five second limit as session start,
  and reads a transcript from its end, so a huge one is quick.
* `--cached` shows the date for a board from another day.
* Hook commands are quoted when sous lives in a folder with spaces.
* Upgrading ends a snooze on an "uncommitted files" row once, because
  those rows now follow the files' content.
* A broken config.toml is reported by new shells instead of "building your
  board now" every time.
* `here` clears git rows as soon as they are gone, instead of after the
  next full board.
* A source that stopped working is named in the headline even when every
  row it found was snoozed.
* `sous report` counts as seen only once it was shown: a failed write, or
  a page no browser could open, keeps its window.
* Two terminals opened together print the board once.
* sous's data files stay private to you, and setup keeps the permissions
  and links of the startup files it edits, and never touches a line that
  is not its own.
* ssh host aliases: tabs, `Include` and `!negation` are understood, and a
  real host (such as gitlab.com on port 443) is never renamed.
* Which notes predate note ids is recorded when your notes file upgrades
  (`threads.json` becomes version 3, so an older sous will refuse it), so
  a retried filing can never match someone else's old item.
* A plugin whose output is partly unreadable, a search that may have been
  cut short, and a project folder sous cannot read all count as missing
  data: earlier rows are kept, marked stale, instead of disappearing.
* A note closed in its tracker that could not be closed in sous stays on
  the board with the reason, instead of disappearing.
* FOLLOWUPS.md: checking a note only reads the file, a file sous cannot
  read is an error rather than "ref missing", and filing and closing use
  the same safe, locked write as everything else.
* The report page is private to you, like the rest of `~/.sous`.
* A failed `gh` or `glab` check shows the tool's own reason; "not logged
  in" only when that is the problem.

### Added
* `sous go --where` prints the project folder only, and `sous go --in
  <folder>` uses a folder already found; the zsh wrapper uses both.

## [0.1.6]

### Changed
* The skill is now a few lines pointing at `sous help`, which carries the
  rules for agents. The CLI is the one source of truth.

## [0.1.5]

### Added
* `sous setup` puts the sous skill in the shared `~/.agents/skills` folder,
  so Gemini CLI, Kimi, Cursor and other agents that read it know sous is
  there, and in Antigravity's skills folder when Antigravity is installed.
* The session start summary opens with one line telling the agent what
  sous is and when to use it.
* The skill names more moments to reach for sous (putting work off,
  waiting on someone, an idea for another project) and tells agents to
  offer a note, asking first.
* docs/commands.md: a guide to every command, option, exit code,
  environment variable and setting. A test keeps it in step with the code.

### Fixed
* With no project folders set, new shells say once what to do instead of
  "building your board now" every time.

## [0.1.4]

### Changed
* `sous setup` now does the whole setup. It finds your projects in the
  usual places (or takes the folders you name: `sous setup ~/code`), and
  adds one line to your zsh, bash or fish startup file so new terminals
  show the board. It says what it did. `--no-shell` leaves your shell alone.
  Installing is now a single command.

## [0.1.3]

### Fixed
* A first run with no config shows a short welcome with the one line to
  add, instead of an error.
* GitHub or GitLab that was never set up on this machine (tool missing or
  not logged in, and nothing in config) no longer puts `?` on every board.
  If it found things before and is now logged out, its rows stay, marked
  stale, and the headline says which source is not set up.
* Multi-line errors from gh or glab are shown on one line.

### Added
* Signal plugins can exit with code 3 to say "not set up here".

## [0.1.2]

### Fixed
* Running `sous setup` after moving sous (for example from a source build to
  Homebrew) updates its agent hooks instead of adding a second set.

## [0.1.1]

### Fixed
* Ticking a box in FOLLOWUPS.md changes one character in place, so a crash
  can never empty the file, and Windows line endings survive.
* A damaged install id is reported instead of silently replaced.
* A git repo at your home folder (dotfiles) no longer claims every folder.
* SSH remotes with a port, and host aliases from `~/.ssh/config`, are
  recognized as GitHub or GitLab.
* A very long line in an agent transcript no longer hides how the session
  ended.
* A broken default GitHub login no longer hides what your configured
  accounts can see, and gh is asked for tokens one at a time, so a gh
  upgrade asks for keychain access only once.
* `sous go` keeps one resume file per project instead of leaving temp files.
* The menu bar script uses the real path to sous, and its rows show when a
  filed note's status could not be checked.
* The session hook cleans up after itself when it runs out of time.

### Added
* Install with Homebrew: `brew install bilal-/tap/sous`.
* Issue templates, a security policy, and private security reports.

### Changed
* Every note now has a stable random id, and new filings mark items with it
  (`<!-- sous:0123456789ab -->`), so notes from two installs can never
  collide in a shared FOLLOWUPS.md. Your notes file upgrades itself; older
  markers keep working.
* A snooze on "files uncommitted" ends when you keep editing, not only when
  the file count changes. Signals may send a `state` fingerprint for this.
* A unique start of a row's id is enough to snooze it (`sous snooze s:0a70`).
* `sous go .` starts your agent in the project you are in, and the zsh
  wrapper finds the project wherever it is on the line.
* New shells run `sous --ambient`, so how often the board prints is set in
  one place: `refresh_hours` in config.toml. Run `sous setup` to update the
  snippet.
* `sous projects` has a header. A board from another day shows the date.
* The README explains why sous matters, with use cases and diagrams, has status badges, and says plainly that sous is a solo hobby project.

## [0.1.0]

The first public release.

### The board and resuming work
* `sous` shows one board of what is waiting on you, on others, and work left
  unfinished, across every project under your folders. Every row says how
  long it has been waiting.
* `sous here` and `sous <project>` show where you left off: branch, last
  commit, how your last agent session ended, notes and ideas.
* `sous report` shows what changed since your last report. `--week` looks
  back seven days, and `--open` shows it as a simple page in your browser.
* The headline never shows a calm zero when data is missing. A failed
  source, a missing folder or out of date rows show as `?` with the reason.

### Notes
* `sous note`, `edit`, `kind`, `snooze` and `done` keep private notes for any
  project, from anywhere, with rough name matching that never guesses.
* `sous file`, `note --file` and `done --close` send a note to the
  project's own tracker, only when you ask: `FOLLOWUPS.md` in the project,
  GitHub issues or GitLab issues. When the item is closed there, your note
  closes too.

### Where sous looks
* git: uncommitted files, unpushed commits, stashes, branches with no
  upstream.
* GitHub, through `gh`: reviews asked of you and changes asked on your
  pull requests, with more than one account supported.
* GitLab, through `glab`: merge requests to review and yours awaiting
  review, on any host you are logged in to.

### Showing up on its own
* Session hooks for Claude Code and Codex show the agent where you left off
  when a session starts. A `sous` skill teaches both agents the commands.
  `sous setup --print-skill` prints it for any other agent.
* A zsh snippet prints the board in new shells, and `sous go` starts your
  agent in a project and leaves your shell there.
* A menu bar view for SwiftBar on macOS.

### For builders
* Plugins in any language for signals, backends and launchers, through the
  same door the built in ones use. The contract is a draft until 1.0; see
  [docs/plugins.md](docs/plugins.md).
* Every command has `--help`, and every command that shows something takes
  `--json`. sous follows the [AXI](https://axi.md) guidelines, checked by
  tests.
* Release builds for macOS and Linux on Intel and Apple silicon, with
  checksums, and an install script.
