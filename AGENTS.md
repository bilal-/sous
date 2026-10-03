# Working on sous

This guide is for every person and agent changing sous. It is short on
purpose. The design lives in [docs/design.md](docs/design.md), and when the
two disagree, the design wins. Plugin authors read
[docs/plugins.md](docs/plugins.md), so change it whenever the plugin
contract changes. Every command and option is described in
[docs/commands.md](docs/commands.md); a test fails when a new one is
missing from it.

## What sous is

A Go command that shows a person what is waiting on them across every
project they work on. It is an index, not a store: it works out git, GitHub
and GitLab state again on every look, and keeps only the person's own notes
and a little history about what it has seen. It can hand a task to a
runner and follow it, but it never does the work or owns a workflow.
Running `sous` with nothing else always shows the board.

## Build and test

    make test      # go vet, then every test with the race detector. Run it before every commit.
    make ci        # formatting check plus make test, as CI runs it
    make build     # bin/sous, with the version from git describe
    make install   # link bin/sous into ~/.local/bin
    go test ./internal/board -update   # only when you mean to change the board's layout

Tests run inside the test process against a throwaway `HOME` and
`SOUS_HOME` (see `internal/cli/testutil_test.go`). When a test needs a real
`sous` process, the test binary plays that part (`SOUS_TEST_AS_BINARY=1`).
**Never** run `sous setup` in a test, and never point a test at the real
`~/.sous`, `~/.claude`, `~/.codex`, `~/.gemini`, `~/.config/opencode` or
`~/.zshrc`. GitHub, GitLab, Claude
Code, Codex, Antigravity and opencode are never called for real; tests
use small fake `gh`, `glab` and agent scripts.

Write the test first, watch it fail, then make it pass. The files in
`internal/board/testdata` pin the exact look of the board and of `here`.
Changing them is a product decision, never a side effect.

Examples, tests and docs use made up names only: `acme/api`, `Sam`,
`git.example.org`. Never real projects, people, hosts or paths.

## Layout

    cmd/sous            main, which calls cli.Run
    internal/cli        the list of commands (verbs.go), argument parsing, output, exit codes. One file per command. No decisions about the data.
    internal/filing     filing a note, closing it upstream, and checking filed notes
    internal/board      builds the board and the here view, sorts rows into sections, draws text and the menu bar, keeps the cache; item.go is the --json read model
    internal/report     what changed since the last report, as text and as a page
    internal/session    the last agent session in each project, and the summary handed to an agent sous starts
    internal/thread     the person's notes
    internal/signal     the signal contract, running signal plugins, what has been seen before, and the git, GitHub and GitLab signals
    internal/backend    the backend contract, FOLLOWUPS.md, and GitHub and GitLab issues
    internal/backend/backendtest   the rules every backend must pass
    internal/signal/signaltest     the rules every signal must pass
    examples/sous-signal-todo      a small plugin in plain shell, for people starting their own
    internal/tracker    gh and glab: which account or host a project uses, running them safely, and the ref format
    internal/tracker/trackertest   fake gh and glab for tests
    internal/harness/harnesstest   a fake agent that ends a run the way every built in agent does
    internal/harness    every agent tool sous works with (Claude Code, Codex, Antigravity, opencode), one file each: skills, hooks and their file layout, hook input, transcripts, headless runs
    internal/launcher   the launcher contract; every harness is a built in launcher
    internal/runs       starting runs, asking runners how they are going, replies, stop and clean
    internal/runner     the runner contract, a built in runner for each harness that runs headless, and their watcher
    internal/runner/runnertest     the rules every runner must pass
    internal/plugin     the one way sous runs another program: find it by name, run it with a time limit; the exit codes every plugin speaks; the registry of each axis's built ins and the door to them
    internal/project    finding projects, reading git facts, matching rough names
    internal/store      saving data files safely: locked, written whole, upgraded in place
    internal/config     config.toml: reading it, the list of settings (from Config's fields), and safe editing (Set), checked to change only what was asked
    internal/hook       what the session hooks do: the project a session is in, and recording how it ended
    internal/install    setup's steps: agent hooks and skills, and the shell line, and Check for each
    internal/doctor     sous doctor: every check, in one list, with how to fix it
    internal/text       how sous writes things for people: ages, times, lines cut to fit
    docs                the guides; docs.go carries commands.md inside the program for each command's --help
    brand               the logo, icons and colours, made by brand/tools (Node, not part of the Go build); see brand/README.md
    internal/testutil   helpers shared by every package's tests

Code only depends downward, in this order: `cli`, then `doctor`, then
`report` and `install`, then `board`, `filing`, `runs`, `hook` and `launcher`, then `signal`, `backend` and `runner`, then
`tracker`, `plugin`, `harness`, `thread`, `session` and `project`, then `store`,
`config`, `text` and `docs`. `board` never imports `filing` or `backend`; it is handed a
function instead. `signal` never imports `backend`; both use `tracker`.
`thread` uses `project` for a project's path and remote; they share a tier.
A built in runner starts one agent in a worktree, records how it ended,
and passes on a reply. Retrying, reviewing and scheduling belong to a
runner plugin such as orchid, never to sous. Only `cli` reads settings
from the environment (`HOME`, `SOUS_*`); the rest take them as arguments.
Finding a program on `PATH`, and handing a child process the environment
sous runs in, are fine anywhere. If a function decides something or owns a
data file, it does not belong in `cli`. `internal/layers_test.go` holds the
code to these rules: a new package takes its place in the order there.

**Built ins.** Each axis has one `Registry` (`signal.Registry`,
`backend.Registry`, `launcher.Registry`, `runner.Registry`): a list of its
built ins, each a name, whether it stays on this machine (`Offline`), and
its calls as `plugin.Op`s. The list drives everything else: `Registry.All`
(every built in, then the plugins config lists), `Registry.Offline` (what
a session start may ask), and the door `sous <axis> <name> <call>`
(`internal/cli/door.go`). The launcher and runner lists are built from
`harness.All`. A built in ends every call with
`plugin.Exit`, so it speaks the same exit codes as a plugin program.

**Adding a command** is its line in `verbs.go` (which says what flags
it takes, how many arguments, and whether it takes `--json`), its
handler in a file of its own, and its section in docs/commands.md,
headed ``### `sous <name>` ``, which is also its `--help`. A command that
changes something answers with `e.changed` (and a `did` the docs list); a
command that shows something answers with `--json` too, never `null`.
The AXI tests loop over every verb, so they check a new one as it lands.

**Adding a tracker** as a built in (Jira, say) is three pieces, each
named after it, and their tests:

1. a row in `tracker.Kinds` (`internal/tracker/kind.go`): its command line
   tool, its public host, its item links, and what `sous doctor` checks.
   If it needs a tool sous does not run yet, the code that runs that tool
   safely goes in `internal/tracker` too, next to `GHRun` and `GLabRun`,
   with a check for doctor in `check.go`, and a setting for its accounts
   or hosts in `config` if it has any.
2. a backend, `internal/backend/<name>.go`: an `IssueCLI` for `Remote`
   (find the project's repo, file, look up, close), and its entry in
   `backend.Registry`.
3. a signal, `internal/signal/<name>.go`: a `RemoteScanner` with its
   queries, and its entry in `signal.Registry`.

The shared code should not need to change; if it does, that is the part
to review closely. `TestTablesAgree` fails until all three exist. Every
backend must pass `backendtest.Run` and `RunUnreachable` against a fake of
its tool that keeps state (`internal/backend/conformance_test.go`, which
fails for a tracker it has no case for), and
`RunDoor` when run as a separate program. A tracker that does not need to
ship inside sous is better as a plugin program (docs/plugins.md).

**Adding an agent** (Gemini CLI, say) is a file in `internal/harness`,
like `claude.go`, and its line in `All`: where its skills and hook settings
live, which events sous hooks and how that file is laid out (`Format`;
`HooksJSON` when it copied Claude Code's), how to read what its hooks send
(`Parse`, which decides what a fresh session is) and its transcript
(`Last`), how its hook answers (`Reply`, when it wants JSON rather than
plain text), whether it is installed (`Present`), and, if it can run
headless, `Headless`. `antigravity.go` is a full example of an agent that
does not copy Claude Code: its own hook file layout, input, answers and
transcript. A hook only some
versions support gets a `Flag`, which becomes a `sous setup --<flag>`
option. setup, doctor, the hooks, `sous go` and runs all read the table.
A skill folder several agents share goes in `harness.SharedSkills`. Its tests use fake input and
transcripts written the way the agent writes them, never the real tool.
Tests name what else it needs, and fail until it is there: an agent that
runs headless adds how it ends a run to `harnesstest.Says` and what a run
of it may do to `TestAgentsMayCommitAndNoMore`; the README and
commands.md name it wherever they list agents (`TestDocsNameEveryAgent`).

## Rules that are easy to break

* **Missing data must never look like zero.** A failed plugin, an unreadable
  repo or a missing folder must show in the headline, and what was known
  before is kept and marked stale. When you add a source, write its failure
  path first.
* **Starting a session stays fast and offline.** `sous here`, the session
  hooks and `sous go` run only the git signal, read everything else from
  what the board saw last, and only check trackers that live in the project
  (`FOLLOWUPS.md`) and the built in runners (which read files on this
  machine). Never add a network call there.
* **Every data file carries a `version`** and is upgraded when read. A file
  from a newer sous is refused, never overwritten. Test every upgrade.
* **Every write is locked and replaces the file whole** (`store.Modify`).
  Several agents writing at once is normal; a test races eight processes.
* **Built ins take the same door as plugins.** Signals, backends,
  launchers and runners are separate programs, and built ins are reached by
  running `sous signal|backend|launcher|runner <name> ...`. No shortcuts
  inside the process.
* **Notes stay private until the person says otherwise.** Anything that
  writes where others can see (`sous file`, `--file`, `--close`) must be
  asked for explicitly.
* **Exit codes:** `0` fine, `1` something failed, `2` wrong command or an
  ambiguous name, `3` asked for the cached board before there was one, or
  the agent or runner `sous go` needs is not set up. Each
  command declares its flags and how many arguments it takes in `verbs.go`,
  and anything else is refused with that command's usage.
* **One dependency** (`github.com/BurntSushi/toml`). A new one needs a
  reason in the commit message.
* **The CLI is the one source of truth.** The skill says only when to use
  sous and to run `sous help`. Commands and agent rules live in `sous help`
  and each command's `--help`, never in the skill, so it never needs
  updating. A test keeps the skill small.
* **Hooks never hold up a session.** `sous hook ...` always exits `0`, prints
  nothing to stderr, and finishes within five seconds whatever git does.

## AXI

sous follows the [AXI](https://axi.md) guidelines for command line tools
that agents use, as they read at this exact version:

* page: https://axi.md, built from `docs/index.html` in
  [kunchenguid/axi](https://github.com/kunchenguid/axi)
* commit: [`e15f82d`](https://github.com/kunchenguid/axi/blob/e15f82dd8e75ff640aaefd5c76c49471c5bcbbea/docs/index.html)
  (2026-09-27), the ten principles
* checked against sous: 2026-10-03 (axi.md at `e7dd8fa` has the same ten
  principles)

We do not follow axi.md automatically. To move to a newer version, read
what changed since that commit, update sous and its tests, then update the
commit above. `internal/cli/axi_test.go` has
one test per principle, and most of them loop over every command, so a new
command is checked without anyone adding a test. When one fails, fix the
command, not the test.

What the tests hold: bare `sous` shows the board; every command that shows
something answers even when there is nothing to show, and its `--json` is
never `null`; a search that finds nothing is an answer, not an error;
unknown flags, and `--json` or `--brief` where a command has no use for
them, fail with exit code `2`; nothing waits for input; every command that
changes something says what it did and what comes next, takes `--json`,
and is safe to retry (`done` twice, the same `note` twice, the same
`go --run` twice); with `--json` an error is JSON on stdout too; every
command's `--help` carries its section of docs/commands.md; the skill can
be printed for any agent and stays tiny, pointing at `sous help`. The
session hooks and the short `here --brief` (five rows of a kind, long
notes cut with `…` and a pointer to `sous show`) are covered in
`hooks_test.go` and the board tests.

Where sous chooses differently, for the person reading the terminal:

* **Plain text, not TOON.** People read the board; `--json` is for programs.
* **Errors go to stderr** as one clear line, and with `--json` to stdout
  as JSON too.
* **No `--fields` or `--full`.** Output is short, a note cut short says so
  and names `sous show <n>`, and `--json` has everything.

Changing one of these, or following a new AXI principle, is a design
change: update this section and `docs/design.md` together.

## Follow-ups

Known rough edges live in [FOLLOWUPS.md](FOLLOWUPS.md), tracked with sous
itself (`sous note --file -p sous "..."`), so they show up in `sous here`.
Do not edit the `<!-- sous:... -->` markers by hand. Tick a box to close one.

## Versions

There are three kinds. The command's version comes from the git tag (see
Releasing). The
plugin contract has its own number (`v`) and freezes before 1.0. Data files
have a `version` each and are always upgraded, never broken. 1.0 is a
promise that the plugin contract is stable, not a measure of popularity.
Add a line to `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md) for every
change a person would notice.

## Releasing

There is one branch, `main`, and it always works: every change lands there
with `make ci` passing. There are no release branches.

Every commit already has its own version. A build from source reports
something like `v0.1.0-5-g21e7aa8`, meaning five commits after 0.1.0.
Nobody bumps a number per commit.

A release is a tag, made when there is something worth handing to people:

1. In [CHANGELOG.md](CHANGELOG.md), rename `[Unreleased]` to the new
   version and start a fresh, empty `[Unreleased]` above it.
2. Commit that, then tag and push:

       git tag v0.1.1
       git push origin main v0.1.1

3. The release workflow tests the tag, builds the downloads for macOS and
   Linux, and publishes them on GitHub with checksums. Then it installs
   the release the way a person does, in a clean home on macOS and Linux
   with zsh and bash (`scripts/install-smoke.sh`): `install.sh`,
   `sous setup` twice (the second must change nothing), and `sous doctor`
   with no problems. If that job fails, the release is broken for new
   people: fix it and release again. You can run the same check by hand,
   in a throwaway home: `scripts/install-smoke.sh v0.1.1 zsh`.
4. The same workflow then updates Homebrew
   (`.github/workflows/tap.yml`): it writes the formula from the release's
   checksums and pushes it to
   [bilal-/homebrew-tap](https://github.com/bilal-/homebrew-tap). If that
   job fails, rerun it from the Actions tab (tap, Run workflow, with the
   version), or run `make tap VERSION=v0.1.1` by hand.
5. Check that GitHub marked the new version as **Latest**
   (`gh release list -R bilal-/sous`). When two tags are pushed together,
   whichever finishes last wins; fix it with
   `gh release edit vX.Y.Z -R bilal-/sous --latest`. The install script
   downloads whatever is marked Latest.

### How Homebrew publishing works

sous is published through its own tap, not Homebrew's main catalogue.

* **The tap** is the public repo
  [bilal-/homebrew-tap](https://github.com/bilal-/homebrew-tap). It holds one
  file, `Formula/sous.rb`. `brew install bilal-/tap/sous` means "the `sous`
  formula in `bilal-/homebrew-tap`".
* **The formula builds nothing.** It points at the release downloads for
  each platform, with their SHA-256 checksums, and installs the `sous`
  binary. Its test runs `sous version`.
* **It is written by `scripts/homebrew-formula.sh`,** which reads the
  release's `checksums.txt`. The tap workflow runs it after every release
  and pushes the result, using the Actions secret `HOMEBREW_TAP_TOKEN` on
  `bilal-/sous`: a fine grained token limited to `bilal-/homebrew-tap`,
  with Contents read and write, and no expiry. If the tap job ever fails
  to push (the token was revoked, say), make a new one the same way and
  replace the secret (`gh secret set HOMEBREW_TAP_TOKEN -R bilal-/sous`).
  `make tap VERSION=vX.Y.Z` does the same by hand. Never edit
  `Formula/sous.rb` by hand; change the script.
* **To check a formula before pushing:** `brew audit --tap=bilal-/tap`, then
  `brew install bilal-/tap/sous` and `brew test bilal-/tap/sous`.

Homebrew's main catalogue (`brew install sous` with no tap) wants projects
that are established and widely used, and builds them from source. Revisit
it once sous has an audience.

Which number to raise, before 1.0:

* **patch** (0.9.0 to 0.9.1): fixes and small improvements.
* **minor** (0.9.x to 0.10.0): new features, or any change to how a command,
  flag, output or the plugin contract behaves. The number after 0.9 is
  0.10, then 0.11: the minor number climbs as high as it needs to.
* **1.0**: only when the owner decides the plugin contract is frozen;
  months away. Never raise it as the next step after a 0.x. After 1.0,
  breaking changes raise the major number.

Only if people ever need fixes on an older line while newer work goes on do
we create a branch such as `release/0.1` from its last tag, and only then.

## Commit messages

A short first line saying what changed, in plain words. Then, when it is
not obvious, a few lines on why.
