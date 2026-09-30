# Writing a plugin

A plugin teaches sous something new: a place where work waits on you, a
tracker to file notes in, a way to start work in a project, or something
that does work in the background. This guide
shows how to write one.

**The contract is version 0, and it is stable.** Everything on this page
is what sous speaks today and will keep speaking. Until 1.0 it only grows:
new optional fields, new optional calls, new exit codes you may use. A
plugin written today keeps working. Anything that would break a plugin
waits for a new contract version, which sous will speak alongside this one
for a while. Freezing the contract for good is what 1.0 means.

Every message carries its version in `v`, so a plugin and sous can always
tell which version they are speaking.

**The quickest start** is the example plugin,
[examples/sous-signal-todo](../examples/sous-signal-todo): about fifty lines
of plain shell that turn `TODO(me)` comments into a row per project. Copy
it and change what it looks for.

## The basics

A plugin is a program named for what it does:

| Kind | Name | Its job |
|---|---|---|
| signal | `sous-signal-<name>` | find things waiting on you, or on others |
| backend | `sous-backend-<name>` | keep notes you file, and report their state |
| launcher | `sous-launcher-<name>` | start an agent or editor in a project |
| runner | `sous-runner-<name>` | do a task in the background, and say how it is going |

Any language works. sous passes arguments, writes to your standard input,
and reads your standard output. Anything you print to standard error is
shown to the person when something goes wrong, so make it a clear sentence.

sous runs a plugin only when it is listed in its settings:

    sous config plugins --add ~/.sous/plugins/sous-backend-jira

(or `plugins = [...]` in `~/.sous/config.toml`).

Each run has a time limit (15 seconds for most calls). When time is up, sous
stops your program and any programs it started.

The built in git, GitHub and GitLab support work through this same door.
You can call them by hand to see real input and output:

    echo ~/code/acme/api | sous signal git scan
    sous backend github status ~/code/acme/api github:acme/api#12

## Signals

sous runs `sous-signal-<name> scan`, then writes project folders to your
standard input, one per line. Print one JSON object per line for each thing
you find:

```json
{"v":0,"id":"s:3fa4c1d2e9b0","project":"/home/sam/code/acme/api","kind":"me","text":"review requested · PR #14 json api","observed":"2026-09-27T09:02:00Z","ref":"github:acme/api#14"}
```

| Field | Meaning |
|---|---|
| `v` | contract version, `0` |
| `id` | starts with `s:`; must stay the same for the same thing on every run |
| `project` | one of the folders you were given |
| `kind` | `me` (waiting on you), `them` (waiting on someone else), `unfinished` (work left behind), `info` (a fact for `sous here`, never a row on the board) |
| `text` | one short line, as the person should read it |
| `observed` | when it began waiting, if you know; sous uses this for its age |
| `ref` | optional pointer to the item, like `github:owner/repo#14` |
| `state` | optional fingerprint of the thing itself; when it changes, a snooze ends even if `text` stayed the same |

A few rules keep the board trustworthy:

* **Keep ids stable.** sous uses the id to remember when it first saw
  something and whether the person snoozed it. A good id is `s:` plus the
  first 12 hex characters of a hash of the project and a key you choose.
* **Report a failure as a failure.** If you cannot reach your service, exit
  with a non zero code and say why on standard error. sous then keeps what
  you reported last time and marks it stale. If you exit `0` with no lines,
  sous takes that to mean nothing is waiting, and clears old rows.
* **Not set up is not a failure.** If the tool or login your plugin needs
  is missing and the person never configured it, exit `3` with the reason
  on standard error. sous stays quiet about it, unless your plugin found
  things before: then those rows are kept, marked stale, and the headline
  says your plugin is not set up.
* A line sous cannot read is counted and skipped, so one stray debug print
  does not throw away everything else.

## Backends

A backend is where a filed note goes: a tracker, a file, anything that can
hold an item and say whether it is still open. sous calls it with one of
five commands.

| Call | Input | Output | Exit codes |
|---|---|---|---|
| `detect <project>` | | | `0` this project is mine, `1` it is not (say why on standard error if it is close, like "not logged in") |
| `file` | JSON on standard input | the new item's ref | `0` filed, `1` failed, `2` request not understood |
| `status <project> <ref>` | | `open`, `closed` or `unknown` | `0` answered, `1` could not find out |
| `close <project> <ref>` | | | `0` closed (closing twice is fine), `1` failed |
| `url <project> <ref>` | | a web link | `0`, or `2` if you have no links. Optional, and not used by sous yet |

The `file` request looks like this:

```json
{"v":0,"id":7,"uid":"0123456789ab","project":"/home/sam/code/acme/api","text":"search index rebuilt on every launch","kind":"me"}
```

`id` is the short number the person types. `uid` never changes, is unique
across installs, and is always sent: use it to recognize the note again.
`legacy` is true only for notes made before sous 0.1.1: a built in backend
then also looks for the older marker it may have left, which carried the
note's number. Ignore fields you do not know; more may be added.

Refuse a `v` you do not know with exit code `2`.

Rules that matter:

* **Filing must be safe to repeat.** sous may send the same `uid` again
  after a crash. Return the same ref and do not create a second item. The built in
  backends leave a small marker such as `<!-- sous:0123456789ab -->` (the
  `uid`) in the item, then look for it before creating anything.
* **Never guess a state.** `unknown` means the item is gone. If you could not
  reach the tracker, exit `1` with the reason instead. sous shows the person
  "status unavailable" and keeps the note open. Answering `closed` would close
  their note, so only say it when it is true.
* **Refs name their backend.** Start refs with your plugin's name, like
  `jira:OPS-12`, so sous knows who to ask about them later.

When sous files a note, it tries each backend's `detect` in order and uses
the first that says yes. A person can pick one for a project in
`config.toml` with `backend = "jira"`.

## Launchers

sous runs `sous-launcher-<name> run <project>` in place of itself, with the
person's terminal. Start your tool in that folder. The environment variable
`SOUS_HERE_FILE` names a file with a short summary of where the person left
off, which you can pass to an agent as its first message.

## Runners

A runner takes a task and works on it somewhere else: an agent in a
worktree, a job on a server, a queue of reviewed changes. sous hands it the
task, then asks from time to time how it is going. It never waits on the
runner, and never checks whether a process is alive: that is the runner's
job.

| Call | Input | Output | Exit codes |
|---|---|---|---|
| `start <project>` | JSON on standard input | the run's ref | `0` started, `1` failed, `2` request not understood, `3` not set up |
| `status <project> <ref>` | | one JSON line | `0` answered, `1` could not find out |
| `stop <project> <ref>` | | | `0` stopped (stopping twice is fine), `1` failed |
| `reply <project> <ref>` | the person's answer on standard input | | `0` carrying on, `1` failed, `2` replies not supported. Optional |
| `clean <project> <ref>` | | | `0` cleaned, `1` failed, `2` not supported. Optional: remove what a finished run left, keeping its work |

The `start` request:

```json
{"v":0,"id":7,"uid":"0123456789ab","project":"/home/sam/code/acme/billing","brief":"Fix the flaky test in checkout_spec…","here_file":"/home/sam/.sous/here/…"}
```

`brief` is the whole task, as the person or their agent wrote it. `id` is
the note's short number, handy for naming a branch. `here_file`, when
present, names a file with a short summary of where the person left off.

The `status` answer:

```json
{"v":0,"state":"needs_you","text":"which fixture should win?","branch":"sous/run-7","worktree":"/home/sam/.sous/runs/…/worktree","log":"/home/sam/.sous/runs/…/log"}
```

`state` is `running`, `needs_you`, `done` or `failed`. `text` is one short
line: the question, the summary, or the reason. The rest are optional.

Rules that matter:

* **`start` returns quickly,** within the usual time limit. Detach the
  work itself; sous never waits on a run.
* **`start` is safe to repeat.** sous may send the same `uid` again. Return
  the same ref and start nothing new.
* **Never guess a state.** If you cannot find out, exit `1` with the
  reason. sous keeps the state it knew and marks the row "status
  unavailable".
* **Refs name their runner,** like `orchid:T3`.
* **Stay in your lane.** A run should never push or publish without the
  person saying so; they review the result.

The built in runners (`claude`, `codex`) are small on purpose: each starts
one agent in a git worktree, records how it ended, and passes on a reply.
They do not retry, review or survive a restart. A runner plugin is where
that belongs.

## Testing your plugin

Three conformance suites check a plugin against every rule above, running
it exactly the way sous does: `internal/signal/signaltest` for signals,
`internal/backend/backendtest` for backends, and
`internal/runner/runnertest` for runners. The example plugin and sous's
own built ins pass them. Go only lets code inside the sous repository
import them, so run them from a small test in a sous checkout:

```go
func TestTodoConforms(t *testing.T) {
	signaltest.Run(t, []string{"/path/to/sous-signal-todo", "scan"},
		[]string{"/path/to/a/project/with/something/to/report"})
}

func TestJiraConforms(t *testing.T) {
	b := backend.Backends("", nil, []string{"/path/to/sous-backend-jira"})[0]
	backendtest.RunDoor(t, b, "/path/to/a/project/it/detects")
}

func TestOrchidConforms(t *testing.T) {
	r := runner.Runners("", nil, []string{"/path/to/sous-runner-orchid"})[0]
	runnertest.RunDoor(t, r, "/path/to/a/git/project", func(ref string) {
		// make the run finish: your runner, your way
	})
}
```

Run your plugin by hand with the examples above too. If it works there, it
works in sous.

## Sharing it

Plugins can live in their own repository. If you would like yours to ship
with sous, or listed in the README, open an issue or pull request. See
[CONTRIBUTING.md](../CONTRIBUTING.md).
