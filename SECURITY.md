# Security

sous runs on your own machine. It reads your git repos, asks `gh` and
`glab` about your GitHub and GitLab work using the logins you already have,
and keeps its local state in `~/.sous` (or `SOUS_HOME`). sous has no hosted
service of its own. Filing or closing a shared note writes to the selected
backend when you explicitly request it.

Agent handoffs include a project summary, which can contain your notes,
and background runs include that summary and your task. The agent may send
this context and project files to its model provider under its own settings.
Plugins run as local programs with your account's access; they may use the
network too. Only plugins listed in your configuration are run.

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
