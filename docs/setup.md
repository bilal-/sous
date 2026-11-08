# Installing and setting up sous

## Install

**On macOS or Linux:**

    curl -fsSL https://raw.githubusercontent.com/bilal-/sous/main/install.sh | sh

This downloads the latest release, checks it against its checksum, puts
`sous` in `~/.local/bin` and runs `sous setup`. If `~/.local/bin` is not on
your `PATH`, the script tells you. For a particular version, run
`curl ... | SOUS_VERSION=v0.1.0 sh`. To put `sous` somewhere else, set
`SOUS_BIN` the same way.

**With Homebrew** (macOS or Linux):

    brew install bilal-/tap/sous && sous setup

**From source**, with Go 1.27.1 or newer (see [go.mod](../go.mod)):

    git clone https://github.com/bilal-/sous && cd sous
    make install && sous setup

You need `git`. GitHub and GitLab are optional:

* For GitHub, install [`gh`](https://cli.github.com) and run `gh auth login`.
  Notifications need a classic token with the notifications scope:
  `gh auth refresh -s notifications`.
* For GitLab, install [`glab`](https://gitlab.com/gitlab-org/cli) and run
  `glab auth login`.

sous uses the logins you already have and never asks for a token.

## Update

`sous update` installs the latest stable release over a release or Homebrew
install. `sous update --check` only checks. A source build is yours to
update: pull the checkout, then run `make install`. Releases older than this
command need one update through the install script or Homebrew first. More
in [the command guide](commands.md#sous-update---check).

## What `sous setup` does

Installing runs `sous setup` for you. It:

* finds your projects in the usual places (`~/code`, `~/src`, `~/projects`,
  `~/workspace`, `~/Developer` and a few more). It looks two folders deep,
  so `~/code` finds `~/code/acme/api`.
* adds session hooks for Claude Code and Codex, and for Antigravity and
  opencode when they are installed. It also adds the `sous` skill for them
  and for any agent that reads the shared `~/.agents/skills` folder (Gemini
  CLI, Kimi, Cursor and others). [Using sous with your agent](agents.md)
  covers each one.
* adds one line to your shell's startup file (zsh, bash or fish), so new
  terminals show the board.
* writes a menu bar script for [SwiftBar](https://swiftbar.app).

It tells you what it did. Open a new terminal and the board is there.

If your projects live somewhere else, say where. You can name more than one
folder:

    sous setup ~/where/my/repos/are

`sous setup` is safe to run again at any time. Add `--no-shell` to leave
your shell's startup file alone.

If something looks wrong later (a `?` in the headline, or an agent that
doesn't know sous), run `sous doctor`. It checks every part of the setup,
including your GitHub and GitLab logins, and gives the command that fixes
each problem.

## Configuration

Change settings with `sous config` (see
[the command guide](commands.md#sous-config)), or edit `~/.sous/config.toml`
by hand:

```toml
roots = ["~/code"]             # where your projects live
ignore = ["scratch/*"]         # folders to skip
agent = "claude"               # the agent sous go starts
runner = "codex"               # the runner sous go --run uses; agent when not set
refresh_hours = 4              # how old the shell's cached board may get
run_minutes = 60               # how long a run by a built in runner may take
gitlab_hosts = ["git.example.org"]   # GitLab servers, beyond the ones glab knows
plugins = ["~/.sous/plugins/sous-backend-jira"]   # extra plugins; only listed ones run
terminal = "auto"              # where sous serve opens a project's tab: auto, none, herdr, tmux, ghostty, iterm, terminal
agent_memory = "on"            # read the memory agents keep for themselves (Claude Code's) into recall; off stops it

[projects."acme/*"]            # every project in the acme folder
github_account = "work-account"

[projects."acme/billing"]      # one project
backend = "markdown"           # always file to FOLLOWUPS.md here
check = "make test"            # what checks a run's work before it is ready
remote_runs = "true"           # a host (a phone through Shellbell) may start runs here
```

If you use more than one GitHub account, `github_account` picks the one `gh`
should use for those projects.

## What works today

sous is young. Here is what it supports, and what it doesn't yet.

| Works today | Not yet |
|---|---|
| macOS and Linux, on amd64 and arm64 | Windows |
| the board in new **zsh**, **bash** and **fish** shells | other shells (run `sous --ambient` from their startup file) |
| session hooks for **Claude Code**, **Codex**, **Antigravity** and **opencode**; the sous skill for them and for **Gemini CLI, Kimi, Cursor** and other agents that read `~/.agents/skills` | session hooks for other agents |
| git: uncommitted files, unpushed commits, stashes, branches with no upstream | |
| **GitHub**: reviews asked of you, changes asked on your pull requests, failing checks on your pull requests, unread notifications that mention or assign you; filing and closing issues | CI on branches without a pull request |
| **GitLab**: merge requests to review, yours awaiting review; filing and closing issues | GitLab to do items |
| `FOLLOWUPS.md` in the project as a simple tracker | Jira, Linear, Gitea, Bitbucket (plugins welcome) |
| sourced project memory, keyword recall, verification, supersession and explicit sharing | automatic transcript extraction and semantic search |
| named efforts linking projects, commitments, memories and dependencies | |
| handing a task to **Claude Code**, **Codex**, **Antigravity** or **opencode** in the background, in its own git worktree, and following it on the board | runs that survive a restart, retries and reviews (a runner plugin's job) |
| the board as a web page on this computer, for mouse and touch, any screen size | |
| a menu bar view through [SwiftBar](https://swiftbar.app) on macOS | a Linux tray icon |
| plugins in any language, on a contract that only grows until 1.0 | a contract frozen for good (that is what 1.0 means) |
| install by script, Homebrew or from source | Windows, other package managers |

## Uninstall

First stop active runs with `sous done <n>`. Review and commit or copy any
work you want to keep before `sous done <n> --clean`. Back up `~/.sous` if
you want to keep your notes and history. Then delete:

* `~/.local/bin/sous` (or `brew uninstall sous`) and `~/.sous`
* the `sous` skill folders in `~/.claude/skills`, `~/.codex/skills`,
  `~/.agents/skills` and `~/.gemini/config/skills`
* the `sous hook` entries in `~/.claude/settings.json` and
  `~/.codex/hooks.json`, the `sous` group in `~/.gemini/config/hooks.json`,
  and `~/.config/opencode/plugins/sous.js`
* the `# sous:` comment and the line under it in your shell's startup files
  (for fish, the file `~/.config/fish/conf.d/sous.fish`)
* the `sous` folder in your cache folder (`~/Library/Caches/sous` on macOS,
  `~/.cache/sous` on Linux)
* if you handed work to agents: merged branches named `sous/run-<n>` in
  those projects, with `git branch -d sous/run-7`. Keep unmerged branches
  until you have reviewed their work, and run `git worktree prune` once the
  worktrees are gone.
