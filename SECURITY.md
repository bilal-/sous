# Security

sous runs on your own machine. It reads your git repos, asks `gh` and
`glab` about your GitHub and GitLab work using the logins you already have,
and keeps its local state in `~/.sous` (or `SOUS_HOME`). sous has no hosted
service of its own. Filing or closing a shared note writes to the selected
backend when you explicitly request it.

`sous approve` is an explicit public action: after review it pushes the result's
commit to the run's branch and creates a draft pull request through `gh` or
`glab`, or reuses an existing pull request. Integration hosts must request
confirmation for that action and bind approval to the commit the person reviewed. Private `done` and `snooze`
actions do not publish work or close upstream items.

Agent handoffs include a project summary, which can contain your notes,
relevant memories and effort links. Background runs also include the task brief.
The agent may send that context and project files to its model provider under
its own settings. Plugins run as local programs with your account's access;
they may use the network too. Only plugins listed in your configuration run.

Private memories and effort definitions live in `memory.json` and `efforts.json`
under the same local state directory. Workspace and project scopes control
automatic context selection, with explicit sharing between projects. They are
not an access-control boundary against programs that can read those files.
An explicit memory ID or broad recall query can read other scopes. Linking a
memory to an effort does not share its body into every linked project's brief.
Forgetting removes stored content and evidence, but cannot erase copies already
included in agent context, exports or backups. Source references are stored
without fetching their contents.

An explicit project name retains its named private scope, while an absolute path
selects its physical checkout. Implicit current-directory selection does not
guess between aliases with different scopes. When discovery is incomplete or
ambiguous, briefings keep local project facts and notes visible, report the
problem, and omit private memory and effort context.

When you hand a task to an agent (`sous go --run`), the built in runners
start Claude Code, Codex, Antigravity or opencode on your machine. Each
works in a separate git worktree, which shares git metadata with the
original repository. Claude accepts file edits and is allowed the git
commands needed to commit; Codex uses a workspace-write sandbox with extra
writable git object, ref, log and worktree metadata directories.
Antigravity uses accept-edits mode, and opencode uses its configured
permissions. See [the command guide](docs/commands.md#handing-work-to-an-agent)
for the details.

The runners tell agents not to push and add git URL rewrites that block
ordinary pushes, including the project's configured remotes. These are
safeguards, not a security boundary: an agent's permissions, sandbox and
other tools determine what it can access or publish. Review the branch
and worktree before accepting a run's result.

Built in runners signal only an authenticated process group for the current
attempt; a saved process ID is not authority to signal. If ownership or completion
evidence is lost, sous reports the run as unavailable and keeps its worktree. The
guarantee covers signalable processes that remain in the owned group; agent
permissions still govern whether a tool can start independent work.

`sous serve` runs only while you keep it open and listens on `127.0.0.1`. The
secret in its printed URL grants browser access and is exchanged for an HTTP-only,
SameSite=Strict cookie. The Host header is checked on every request; non-GET API
requests also require the page's Origin. The browser can read private board data and
invoke the advertised actions, so keep that URL and the local `serve.token` file
private. This is local browser access, not a hosted account or a boundary against
other programs running as your user.

`sous update` and `sous update --check` contact GitHub's public release
endpoint; no private context or credentials are sent. Release downloads use
HTTPS and are checked against the release's SHA-256 checksums before atomic
replacement. These checksums detect corruption; they are not a separate
signature authority. Homebrew installations delegate to Homebrew. Source
builds are never overwritten by the release updater. No automatic update
checks run in session hooks or project views.

## Reporting a problem

If you find a security problem, please tell me privately rather than in a
public issue: use
[Report a vulnerability](https://github.com/bilal-/sous/security/advisories/new)
on GitHub. Include what you found and how to see it happen.

sous is a solo hobby project, so I cannot promise a fixed response time,
but security reports come first and I will reply as soon as I can. Once a
fix is released, I am happy to credit you if you would like.

## Supported versions

Only the latest release gets fixes. Before 1.0 there is one line of
releases, so updating to the newest version is the way to get a fix.
