# Delivery record: sous support for builds from the phone

2026-10-25 · companion to [autonomous-build.md](autonomous-build.md) and [protocols-v1-draft.md](protocols-v1-draft.md)

> Historical plan and delivery record, last updated on 2026-10-26. Read the
> [planning archive](README.md) for current behavior and subsequent contract
> changes. The round acceptance criteria below preserve the original plan;
> the status section records the changes that actually shipped.

**Done means:** a host (the Shellbell service, Herdr, a test) can start a run in an allowed project, watch it move through phases, answer its question, and approve its exact result commit as a draft PR, through `sous integration` v1 and runner door v1, with every step safe to retry after a crash. Herdr and v0 runners keep working unchanged.

The original plan ended each round with `make ci` green, docs/commands.md and docs/plugins.md current, a commit, and a release (push, tag, GitHub release).

## Decisions folded in from the protocol review

| Topic | Decision |
| --- | --- |
| C2 wire version | stays `v: 0`. New calls and fields are turned on only by a valid `describe` answer. sous never sends a field a runner hasn't said it understands |
| v0 detection | a runner is v1 only if `describe` exits 0 with a parseable line; anything else is v0 |
| Exit 4 | a typed conflict in `plugin.Call`, mapped to `conflict` outcomes |
| C1 selection | `--protocol 1` on every call, carried as a placeholder in the advertised argv before `--`. v0 output, schema URL and types unchanged |
| Action scope | each v1 action declares `scope`: `global`, `project` or `item` |
| `op` record | per sous home. Reserved on disk before acting, finalised after, locked while in progress (`in_progress`, exit 5). Input hash over action, target, parameters and stdin bytes. Only final outcomes recorded; `failed` may be retried. `run` and `approve` records kept for good, others 30 days, then a tombstone answers `op_expired` |
| `op` and runs | sous maps `op` to the start `uid` and passes `op` to runner `reply` and `approve` so the runner can keep receipts |
| Questions | `question.id` plus an opaque `question.token` and optional `expires_at`; `reply` must name both |
| Phases | add `blocked` with a `reason` (rate limited, sign-in, tool missing); add `queued → failed` and `needs_you → failed`; `published` is sous's overlay, never a runner phase |
| v0 runners in C1 | no `phase`, no `result`, no `approve` in their actions |
| Built-in runners | learn C2 v1: phases, `result` with a clean commit, `checks` from the project's check command |
| Approve | one action, recorded as checkpoints (accepted, pushed, pr). Pushes the exact commit to a branch under `sous/` or `<runner>/`, never forced, refuses history that holds `.orchid/` |
| Watch | v1 sends changed items only, metadata replacements, a per-stream `seq` and `--max-bytes` trimming with `omitted` counts; `fresh` flips change the revision |
| Diffs | separate paged `diff` read action pinned to base and commit |
| Credentials | documented as "fewer credentials in the environment", not a sandbox; checks and review shown as runner-reported |

## Status (2026-10-26)

All seven rounds shipped: R1 v0.13.0, R2 v0.14.0, R3 v0.15.0, R4 v0.16.0, R5 v0.17.0, R6 v0.18.0, R7 v0.19.0. Version 1 still says `draft: true`, so Shellbell's implementation can still shape it before it is frozen.

Not done, on purpose or for later:

* `sous integration check` (replaying a host's calls) was dropped: the v1 schema, the validated examples and `sous runner check` cover conformance.
* Expiring op records: ops are kept for good instead (they are tiny), so `op_expired` is never answered.
* The built in runners do not keep the computer awake; Orchid's O10 covers its own runs.
* Publishing does not record the PR as the note's tracker ref.

## Rounds

| Round | Contents | Change IDs | Accept when |
| --- | --- | --- | --- |
| R1 Contracts | protocol draft v2 in docs; C2 `describe`, exit 4, v1 status fields read when advertised; `sous runner check`; C1 `--protocol 1`, v1 schema beside v0, action scope and typed parameters, argv placeholders, example files, `sous integration check` | S13, S14 | Herdr and v0 runner tests pass untouched; v1 example files validate; `sous runner check` passes the built-ins and the fake |
| R2 Run from a host | `op` store; `run` and `stop` actions; `remote_runs`; run fields (`phase`, `progress`, `question`, `result`); `reply` with question token; built-in runners report phases | S1, S2, S4, S5 | a run started twice with one `op` makes one run, including after a simulated crash between reserve and finalise |
| R3 Stream | watch deltas, `seq`, `--max-bytes`, trimming | S12 | no watch line exceeds the cap; a gap is recoverable from a fresh snapshot |
| R4 Board | Needs you, In progress, Ready to approve groups; run view in `sous show` | S6 | golden board files updated on purpose |
| R5 Approve | built-in result commit and checks; `approve`, `discard`, `write_public`, publication checkpoints, `diff` action | S7, S8, S9 | approve retried after each checkpoint publishes once; a moved branch is refused |
| R6 Plugin runners live | watch polls plugin runners; per-project `runner` | S3, S10 | an external runner's phase change shows in watch without `sous show` |
| R7 Unattended checks | `sous doctor` checks for remote runs | S11 | doctor names every missing piece with its fix |

## Round 2 (2026-10-26): the board everywhere

* sous v0.20.0: `sous serve` (web board, checked in Chrome: dark, light, narrow, new task round trip, diff viewer), `integrations/design` tokens and display rules, `run_projects` in v1 snapshots, terminal fixes.
* Shellbell: branch `feature/sous-board` in a separate worktree (from main, one commit, not pushed, not merged, no release at this checkpoint). Protocol `board-v1`, the service's BoardHost, the phone's board screen, new task sheet, diff sheet, App Lock, changeset (computer and protocol minor; mobile stays 1.0.0).
* Not done: run alerts through `agent-blocked` / `agent-finished` (B4); the phone screen has not been run on a device or simulator, only typechecked, linted and tested; mobile launch-train notes not updated.
