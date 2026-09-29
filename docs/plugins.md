# Writing a plugin

A plugin teaches sous something new: a place where work waits on you, a
tracker to file notes in, or a way to start work in a project. This guide
shows how to write one.

**The contract is a draft (v0).** It may still change before 1.0. Every
message carries a version field `v`, so a plugin and sous can always tell
which version they are speaking, and a change will never be silent.

## The basics

A plugin is a program named for what it does:

| Kind | Name | Its job |
|---|---|---|
| signal | `sous-signal-<name>` | find things waiting on you, or on others |
| backend | `sous-backend-<name>` | keep notes you file, and report their state |
| launcher | `sous-launcher-<name>` | start an agent or editor in a project |

Any language works. sous passes arguments, writes to your standard input,
and reads your standard output. Anything you print to standard error is
shown to the person when something goes wrong, so make it a clear sentence.

sous runs a plugin only when it is listed in `~/.sous/config.toml`:

```toml
plugins = ["~/.sous/plugins/sous-backend-jira"]
```

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
| `kind` | `me` (waiting on you), `them` (waiting on someone else), `unfinished` (work left behind) |
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
across installs, and is always sent: use it to recognize the note again. `legacy` is true only for notes made before
sous 0.1.1: a built in backend then also looks for the older marker it may
have left, which carried the note's number.

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

## Testing your plugin

The conformance suite in `internal/backend/backendtest` checks a backend
against every rule above, and `RunDoor` drives your program exactly the way
sous does. Go only lets code inside the sous repository import it, so run it
from a small test in a sous checkout:

```go
func TestJiraConforms(t *testing.T) {
	b := backend.Backends("", nil, []string{"/path/to/sous-backend-jira"})[0]
	backendtest.RunDoor(t, b, "/path/to/a/project/it/detects")
}
```

Run your plugin by hand with the examples above too. If it works there, it
works in sous.

## Sharing it

Plugins can live in their own repository. If you would like yours to ship
with sous, or listed in the README, open an issue or pull request. See
[CONTRIBUTING.md](../CONTRIBUTING.md).
