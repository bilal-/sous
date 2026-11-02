# Protocols v1 (draft)

2026-10-25 · draft for review · companion to [autonomous-build.md](autonomous-build.md)

> Historical contract proposal. The [planning archive](README.md) lists the
> decisions that changed before implementation. Use [host integrations](../integrations.md),
> [plugin contracts](../plugins.md) and their schema examples when implementing
> a consumer; the JSON and proposed commands below are not current fixtures.

Three contracts join sous, Orchid and Shellbell. Each has one owner. A repo implements its side against the schema and the example files, and is done when its conformance check passes. No repo needs another repo's code to build or test.

| Contract | Owner | Between | Today |
| --- | --- | --- | --- |
| C1 `sous.integration` v1 | sous | sous ↔ hosts (Shellbell service, Herdr plugin) | v0, `integrations/schema.json` |
| C2 runner door v1 | sous | sous ↔ runners (built-in runners, `sous-runner-orchid`) | unversioned, `docs/plugins.md` |
| C3 `board-v1` | Shellbell | Shellbell service ↔ Shellbell mobile | does not exist |

## 0. Shared rules

**Encoding.** JSON, UTF-8. Times are RFC 3339 UTC strings. Ids are opaque strings compared for equality only.

**Versions.** Every top-level object carries `v`. Within a major version, changes are additions only: new optional fields, new actions, new enum values listed below as *open*. Removing, renaming or changing the meaning of anything needs a new major. During a major transition the producer serves both majors for at least one release.

**Unknown and missing.** Readers ignore unknown fields and unknown actions. An *open* enum may gain values; readers treat an unknown value as `unknown` and show it as such. A missing value is never shown as zero, empty or "fine".

**Identities that cross every boundary.**

| Name | Made by | Form | Meaning |
| --- | --- | --- | --- |
| `op` | the phone (or any caller) | 16–64 chars, `[A-Za-z0-9_-]`, random | One requested action. Same `op` + same input = same outcome, forever. Same `op` + different input = refused. |
| `key` | sous | `n:<uid>` or `s:<id>` | One board item. Stable across restarts. |
| `ref` | the runner | `<runner>:<id>` | One run, as the runner knows it. |
| `question.id` + `question.gen` | the runner | string + integer | One question. `gen` increases whenever the question's text or choices change. |
| `result.commit` | the runner | 40-hex git commit | The exact content a person reviews and approves. |
| computer | Shellbell | existing computer fingerprint | Which machine. Keys are only unique per computer. |

**Run lifecycle.** One set of phases, used by C1 and C2.

| Phase | Set by | Next |
| --- | --- | --- |
| `queued` | runner | `planning`, `working`, `stopped` |
| `planning` | runner | `working`, `needs_you`, `failed`, `stopped` |
| `working` | runner | `reviewing`, `needs_you`, `ready`, `failed`, `stopped` |
| `reviewing` | runner | `working`, `needs_you`, `ready`, `failed`, `stopped` |
| `needs_you` | runner | any working phase after a reply, `stopped` |
| `ready` | runner, only when required checks passed | `approved`, `discarded`, `stopped` |
| `approved` | runner, on `approve` | `published` (sous), `approved` again on retry |
| `published` | sous, after push + draft PR | terminal |
| `discarded` | runner, on `discard` | terminal |
| `failed` | runner | terminal |
| `stopped` | runner, on `stop` | terminal |

`phase` is an open enum. v0's `state` stays, derived from `phase`: `queued`/`planning`/`working`/`reviewing` → `running`; `needs_you` → `needs_you`; `ready`/`approved`/`published` → `done`; `failed`/`stopped`/`discarded` → `failed`.

## C1. `sous.integration` v1 (owner: sous)

### Discovery and version choice

`sous integration --json` gains `versions: [0, 1]`. A host asks for v1 by adding `--protocol 1` to every advertised command. Without it, sous answers v0 exactly as today, so the Herdr plugin keeps working unchanged.

### Item: the `run` object (v1)

All v0 fields stay. Added, all optional:

```json
"run": {
  "runner": "orchid", "state": "running", "phase": "reviewing",
  "progress": {"done": 3, "total": 7, "step": "reviewing T4"},
  "question": null,
  "result": null,
  "queued_reason": null,
  "updated_at": "2026-10-25T18:02:11Z",
  "fresh": true,
  "...": "v0 fields: text, branch, worktree, log, key, brief, checked_at, log_tail, error"
}
```

```json
"question": {"id": "q-12", "gen": 2, "text": "Which fixture should win?",
             "choices": ["keep old", "use new"], "other": true}
```

```json
"result": {
  "summary": "Retry note writes on EBUSY",
  "base": "9f1c…", "commit": "4ab2…", "branch": null,
  "files": {"changed": 4, "added": 1, "removed": 0, "lines_added": 88, "lines_removed": 12},
  "checks": [{"name": "go test ./...", "status": "passed"}, {"name": "lint", "status": "not_run"}],
  "review": {"by": "codex", "independent": true},
  "pr": null
}
```

- `fresh` is false when sous could not get a status answer in the last poll; the host shows the age from `updated_at`.
- `checks[].status` is `passed`, `failed` or `not_run` (open enum).
- `branch` and `pr` are filled after approval and publishing.
- `section` is unchanged (still closed). Hosts derive "Needs you / In progress / Ready to approve" from `run.phase`.

### Actions (v1)

The action record gains:

| Field | Meaning |
| --- | --- |
| `parameters` | now a list of `{name, type, required, max_bytes}`; `type` is `id`, `project`, `string`, `text`, `commit`, `question` |
| `input` | `"stdin"` when one `text` parameter is sent on standard input, else absent |
| `effect` | `read`, `launch`, `write_private`, **`write_public`** (open enum) |
| `confirm` | true when a host must ask before running it (all `write_public`) |
| `op` | true when the action takes `--op <op>` and is safe to retry under it |

New and changed actions (all take `--op`):

| id | Parameters | Effect | What it does |
| --- | --- | --- | --- |
| `run` | `project`, `brief` (stdin, ≤ 16 KB) | launch | `sous go <project> --run - --key <op>`; refused unless the project has `remote_runs = true` |
| `stop` | `id` | write_private | asks the runner to stop the run |
| `reply` | `id`, `question` (`<id>:<gen>`), `answer` (stdin, ≤ 4 KB) | write_private | refused if the run's current question is not `<id>:<gen>` |
| `approve` | `id`, `commit` | write_public | runner `approve`, then push the returned branch and open a draft PR; refused if `commit` ≠ the run's `result.commit` |
| `discard` | `id` | write_private | runner `discard` |

### Action outcome

Every v1 action prints one JSON object and exits 0, 1, 2, 3 or 4:

```json
{"kind": "outcome", "v": 1, "op": "a1b2…", "action": "approve", "id": "88",
 "outcome": "done", "item": { "...": "the item after the action" }}
```

| `outcome` | Exit | Meaning |
| --- | --- | --- |
| `done` | 0 | performed now |
| `already_done` | 0 | this `op` was performed before; same result returned |
| `refused` | 2 | not allowed (`reason`: `not_allowed_project`, `op_reused`, `bad_input`, `unknown_action`) |
| `conflict` | 4 | state moved on (`reason`: `stale_question`, `commit_changed`, `wrong_phase`) |
| `failed` | 1 | tried and failed; `reason` says why; safe to retry with the same `op` |
| `not_ready` | 3 | sous or the runner is not set up |

sous records each `op` with a hash of its input and its outcome in `~/.sous`, keeping them 30 days.

### Watch (v1)

`sous integration watch --json --protocol 1 [--max-bytes N]`

- First line: `{"kind":"event","v":1,"type":"snapshot",…}` with the whole snapshot.
- Then `type: "changes"` events carry `revision`, `previous_revision` and only the changed items. **No full snapshot** (v0 sends one with every change).
- `--max-bytes N` (minimum 16384): no line is longer than N bytes. If a snapshot would be, sous drops the oldest items of the lowest-priority sections first and adds `omitted: {"unfinished": 12, "on_others": 3}`. If a change event would be, sous sends a fresh (trimmed) snapshot instead.
- Watch asks plugin runners for status every `poll_interval_ms` (default 5000 for plugin runners), each within the 3 s status limit, and emits changes. A runner that doesn't answer leaves its runs with `fresh: false`.
- A host that sees a `previous_revision` it doesn't hold restarts watch to get a snapshot.

### Conformance

- `integrations/schema.json` v1, with v0 kept as `integrations/schema.v0.json`.
- `integrations/examples/` holds one file per case: empty board, full board, run through every phase, a question answered, a stale reply, an approve retry, a trimmed snapshot.
- `sous integration check --host-log <file>`: replays a host's recorded calls and checks argument use, `--op` reuse and `--protocol` on every call.

## C2. Runner door v1 (owner: sous)

### Version choice

A new optional call: `describe` → one JSON line `{"v": 1, "calls": ["start","status","stop","reply","clean","approve","discard"], "phases": true}`. A runner that has no `describe` (exit 2) is v0 and is used exactly as today.

### Calls and limits

| Call | Input | Output | Limit | Exit |
| --- | --- | --- | --- | --- |
| `describe` | – | JSON | 3 s | 0, 2 = v0 runner |
| `start <project>` | JSON on stdin | ref | 15 s | 0, 1, 2 bad request, 3 not set up |
| `status <project> <ref>` | – | JSON line | 3 s | 0, 1 could not find out |
| `stop <project> <ref>` | – | – | 15 s | 0 (twice is fine), 1 |
| `reply <project> <ref>` | JSON on stdin (v1) or raw text (v0) | – | 15 s | 0, 1, 2 unsupported, **4 conflict** |
| `approve <project> <ref>` | JSON on stdin | JSON | 15 s | 0, 1, 2 unsupported, 4 conflict |
| `discard <project> <ref>` | – | – | 15 s | 0, 1, 2 unsupported |
| `clean <project> <ref>` | – | – | 15 s | 0, 1, 2 unsupported |

### Requests and answers (v1)

`start` stdin, unchanged plus `v: 1`. `uid` is already the retry key: the same `uid` returns the same ref and starts nothing new.

```json
{"v": 1, "id": 7, "uid": "0123456789ab", "project": "/home/sam/code/acme/billing",
 "brief": "Fix the flaky test in checkout_spec…", "here_file": "/home/sam/.sous/here/…"}
```

`status` answer: v0 fields plus `phase`, `progress`, `question`, `result`, `queued_reason`, in the shapes of C1. `state` stays required for v0 readers.

`reply` stdin:

```json
{"v": 1, "question": {"id": "q-12", "gen": 2}, "answer": "use new"}
```

Exit 4 if the run's current question is not `q-12` generation 2.

`approve` stdin and answer:

```json
{"v": 1, "commit": "4ab2…"}
```
```json
{"v": 1, "branch": "orchid/result/r-014", "commit": "4ab2…", "base": "9f1c…"}
```

The runner checks `commit` is its current `result.commit` (else exit 4), records acceptance, and makes `branch` exist locally pointing at `commit`. It never pushes. A repeat returns the same answer. sous then pushes `branch` and opens a draft PR, looking up an existing PR by branch first.

### Rules

- **Never push, never publish.** sous runs runners with an environment that has no `GH_TOKEN`, `GITLAB_TOKEN`, `SSH_AUTH_SOCK` or cloud credentials.
- **Never guess a state.** If status can't be found out, exit 1; sous marks the run `fresh: false`.
- **Status must answer in 3 s.** Read your own state; don't run long commands.
- **Stop is durable.** After `stop` exits 0, no later status may report a working phase for that ref.
- **The result is immutable.** Once a run reports `ready` with a `result.commit`, that commit doesn't change. New work means a new commit and a new `ready`.

### Conformance

`sous runner check <path> [--project <repo>]` runs the existing `runnertest` suite from the command line, plus v1 cases: phases move only along the lifecycle, `reply` with a stale question exits 4, `approve` with a wrong commit exits 4, `approve` twice returns the same answer, `stop` then `status` never reports working.

## C3. `board-v1` (owner: Shellbell)

### Feature

`board-v1` in `hello.features` and `conn.hello.features` (the list allows 8; 4 are used today). The service advertises it only while its sous reader is healthy. Neither side sends board messages unless both advertised it.

### Messages

C3 carries C1 without reinterpreting it: sous's lines travel as strings, and only the phone parses them, with its own C1 v1 parser.

| Message | Direction | Fields |
| --- | --- | --- |
| `board.update` | computer → phone | `seq` (integer, +1 per message), `line` (one C1 watch line, ≤ 48 KB) |
| `board.resync` | phone → computer | – ; the service sends the latest snapshot line, then continues |
| `board.action` | phone → computer | `op`, `action`, `params` (object of strings), `input` (string ≤ 16 KB, optional) |
| `board.result` | computer → phone | `op`, `line` (the C1 outcome JSON) or `error` (`not_listed`, `locked`, `unavailable`, `timeout`) |

- The service runs a `board.action` only if `action` is in the latest `describe` and in the target item's `actions`, adding `--protocol 1 --op <op>`.
- `board.action` is a control message: App Lock blocks it while the app is locked (`control-lock.ts`).
- The phone keeps each unanswered `op` and its input on disk and resends the same `op` after a reconnect. sous's `op` record makes the repeat safe.
- The phone treats a `seq` gap as a reason to send `board.resync`.

### Alerts

No new reason. The service raises `agent-blocked` when a run's phase becomes `needs_you`, and `agent-finished` when it becomes `ready`, `failed` or `published`. Context: `repository` = project name, `title` = the brief's first line, `agentName` = `sous`. The session id is `sous:<key>` (≤ 128 chars); the phone routes `sous:` ids to the board card.

### Conformance

Shellbell's protocol tests replay the C1 example files through the service and the phone's parser, and check: a full lifecycle renders, a `seq` gap triggers resync, a locked app sends no action, a repeated `op` after reconnect gets `already_done`.

## Open questions for review

1. Should C3 carry C1 lines as opaque strings (the draft), or as typed Shellbell messages?
2. Is `op` per action enough for idempotency, or does each boundary need its own id?
3. Should `approve` be one C1 action (sous does runner approve + push + PR), or two steps the host can see?
4. Is a 30-day `op` record right? What happens to an `op` retried after it expired?
5. Is `phase` complete? Missing: `paused` (rate limit), `blocked` (sign-in expired)?
6. Should hosts get diffs (`files` and patch pages) through C1, or through a separate `sous show --diff` action?
7. `--protocol 1` on every call, or one negotiated session?
