# Security

sous runs on your own machine. It reads your git repos, asks `gh` and
`glab` about your GitHub and GitLab work using the logins you already have,
and keeps its notes in `~/.sous`. It never sends your data anywhere else.

When you hand a task to an agent (`sous go --run`), the built in runners
start Claude Code or Codex on your machine with the permissions you already
gave them, plus what committing on the run's own branch needs. The agent
works in its own git worktree, and its pushes are blocked through each of
the project's remotes. That block is a safeguard, not a sandbox: the
agent's own permissions and sandbox are what keep it in bounds.

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
