# Contributing to sous

Thank you for helping. Every kind of contribution is welcome: a bug report,
an idea, a fix, a new connector, or a clearer sentence in the docs.

A quick word on what to expect: sous is a solo hobby project, looked after
in spare time. Every issue and pull request is read, but I cannot promise to
answer or fix everything, or to do it quickly. Small, focused pull requests
with a test are the easiest to say yes to.

## Good ways to start

* **Write a connector.** sous gets more useful with every place it can see.
  Signals for Jira, Linear, Gitea, Bitbucket, Azure DevOps, Sentry or CI
  status, backends for any tracker, and runners that do work in the
  background (on a server, or through a tool like orchid) are all wanted.
  [docs/plugins.md](docs/plugins.md) shows how.
* **Report what surprised you.** If the board showed something wrong, or
  missed something it should have shown, open an issue with what you saw
  and what you expected. Output from `sous --json` helps a lot. Please
  replace real names and paths first.
* **Pick up a follow-up.** [FOLLOWUPS.md](FOLLOWUPS.md) lists known rough
  edges. Unticked items are open.

For anything large, open an issue first so we can agree on the shape before
you spend time on it.

## Working on the code

You need Go 1.27 or newer and `git`.

    make build     # build bin/sous
    make test      # go vet, then every test with the race detector
    make ci        # formatting, vet and tests, as CI runs them

Before you send a change:

* **Read [AGENTS.md](AGENTS.md).** It explains how the code is laid out and
  the few rules that keep sous trustworthy. The most important: missing data
  must never look like zero, and `sous here`, the session hooks and
  `sous go` must never wait on the network.
* **Write the test first.** Show the problem with a failing test, then fix
  it. Tests never touch your real `~/.sous`, `~/.claude`, `~/.codex` or shell
  files, and never call real GitHub, GitLab, Claude Code or Codex. They use
  small fake `gh`, `glab`, `claude` and `codex` scripts instead.
* **Use made up data.** Examples, tests and docs use invented projects,
  people and hosts (`acme/api`, `Sam`, `git.example.org`). Never real ones.
* **Keep `make ci` passing**, and add a line to
  [CHANGELOG.md](CHANGELOG.md) when people will notice the change.
* **Add no new dependencies** without a good reason in the commit message.
  sous has one today.

## Adding a built in tracker

A new built in tracker is usually two small tables: one for filing
(`internal/backend/<name>.go`) and one for finding work
(`internal/signal/<name>.go`). The shared code does the rest. Add a fake of
the tracker's command line tool and run it through the conformance suite in
`internal/backend/conformance_test.go`. AGENTS.md has the full recipe.

## Pull requests

Keep each pull request to one idea. Say what changed for the person using
sous, and how you tested it. Small, focused changes are reviewed fastest.

## License

sous is licensed under the [MIT License](LICENSE). By sending a
contribution you agree that it is shared under that license. There is no
separate agreement to sign. Keep existing notices, and follow
[TRADEMARK.md](TRADEMARK.md) for the name and logo.

## Be kind

Assume good intent, be patient with newcomers, and keep feedback about the
work, not the person.
