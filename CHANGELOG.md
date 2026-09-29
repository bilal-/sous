# Changelog

Every change people will notice is listed here, newest first. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Before 1.0,
commands and flags may change between minor versions; data files are always
upgraded, never broken.

## [Unreleased]

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
* The README has status badges and says plainly that sous is a solo hobby project.

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
