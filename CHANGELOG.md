# Changelog

Every change people will notice is listed here, newest first. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Before 1.0,
commands and flags may change between minor versions; data files are always
upgraded, never broken.

## [Unreleased]

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
