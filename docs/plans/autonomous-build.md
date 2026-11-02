# Autonomous build from the phone

2026-10-25 · historical proposal

> This proposal predates the shipped sous changes. Read the
> [planning archive](README.md) for the current contracts and the decisions
> that superseded this draft. Its milestones and source references describe
> the repositories at the draft date, not their current delivery status.

Type a task on your phone, and an agent on your own computer does it, like a cloud coding worker that runs on your PC. You watch it, answer its questions, and approve a checked result with one tap. The first slice uses sous's built-in Claude runner and needs no Orchid. Orchid comes in second, as the worker for bigger builds. This is version 2, revised after four independent reviews.

## What the reviews changed

Four reviewers (Claude Opus 5.5, Claude Fable 5.1, Codex gpt-6-astra, Codex gpt-6.1-sol) read version 1 against the three repos. All four said the split is right but the plan wasn't ready to build, and they named the same problems.

| Version 1 said | The code says | Version 2 |
| --- | --- | --- |
| The `watch` stream keeps Orchid runs live | `watch` only reads local state and skips plugin runners (`sous/internal/runs/runs.go:99`) | `watch` asks plugin runners for status on its interval (S3) |
| Approve pushes `orchid/integration` | Orchid's pre-push hook blocks it, its history holds `.orchid/` state, and work from every run builds up on it | The runner produces a clean branch per run tied to the reviewed commit, and calls `run accept` (O3, O4) |
| sous runs a queue | sous's `AGENTS.md`: scheduling belongs to a runner plugin, never to sous | Orchid's machine-wide pump owns the queue; sous only shows `queued` (O7) |
| Headless planning comes first | The phone loop already works on the built-in runner except for a `run` action | Phone loop first (M1), Orchid second, headless planning third |
| Worktree limits keep the agent contained | Orchid documents no filesystem or network sandbox | Treat a phone start as remote code execution (Security) |
| Phone requests are de-duplicated | Shellbell's request memory is lost on restart | Every start sends `sous go --key` with a key the phone generated (S2, B3) |
| Free-text answers need building | Orchid already accepts them when a question has no choices; sous passes text through | Only "choices plus Other" is new (O6) |

## How it fits

Each product keeps one job and still installs alone. sous is the index and the front door: it shows every repo's board, sends your actions to the right runner, and is the only thing that publishes. The runner owns how work runs, including the queue, cancellation, acceptance and the result. Shellbell is the screen and the alerts.

```
Phone · Shellbell mobile         board, new task, answers, approve
   │ new task · answer · approve          ▲ board-v1 update · push alert
   ▼                                      │
Shellbell service                on your computer; runs only actions sous lists
   │ sous go --run - · sous reply         ▲ sous integration watch --json
   ▼                                      │
sous                             board across repos; shows runs; publishes
   │ runner start · reply · stop · approve ▲ runner status: tasks, questions
   ▼                                      │
Runner: built-in Claude, then Orchid   does the work; owns queue, accept, clean branch
   │ on approve: sous pushes the clean branch
   ▼
Your repo                        draft PR, pushed only when you approve
```

The phone never sends a command line. It sends an action id that sous itself listed, and every call on the way down is safe to retry. That matters on a mobile connection that drops.

## Contracts first

Define the contracts before any feature code, then each repo builds its side on its own schedule. Each contract has one owner repo, a versioned schema file, example files that both sides test against, and a conformance check written in no particular language. A repo is done with its side when it passes the check, not when the other side happens to work with it.

| Contract | Owner repo | Defined in | Conformance | Implemented by |
| --- | --- | --- | --- | --- |
| **C1 `sous.integration` v1:** describe, snapshot, watch, actions, the `run` item field | sous | `integrations/schema.json` plus example files in `integrations/examples/` | `sous integration check <host log>` for hosts; schema tests in sous | sous (producer); Shellbell service and Herdr plugin (hosts) |
| **C2 Runner door v1:** start, status, stop, reply, clean, plus optional approve and discard; the status fields phase, progress, question and result | sous | `docs/plugins.md` plus `docs/runner.schema.json` | `sous runner check <path to runner>`: the existing Go conformance suite, now runnable from the CLI so a bash runner can use it | sous (caller); built-in runners and `sous-runner-orchid` (runners) |
| **C3 `board-v1`:** `board.update`, `board.action`, `board.result` | Shellbell | `packages/protocol/src/inner.ts`; the payload of `board.update` is a C1 watch line, unchanged | Shellbell protocol tests, fed the C1 example files | Shellbell service and mobile app |

Three rules keep this workable:

1. **Only additions within a major version.** New optional fields and actions are fine; renaming, removing or changing the meaning of anything needs a new major (`v: 2`), and both sides support the old one for a release.
2. **Unknown things are ignored, missing things are never zero.** Every side skips fields and actions it doesn't know, and shows "unknown" rather than a default when something expected is absent.
3. **Example files are the source of truth for behaviour.** Each contract ships example inputs and outputs covering the lifecycle below, including a retry and a stale question. Consumer repos pin a released copy of them and test against it, so no repo needs another repo's code to run its tests.

Today C1 is v0 and C2 has no version number. The step before M1 (M0) brings both to v1 with the additions this plan needs, and publishes the example files and the two check commands.

**One lifecycle across all three.** Every run carries the same identity end to end: the phone's operation id becomes the sous key, sous records the runner's ref, and the runner reports a result with a base commit and a result commit. Approval names that result commit. Every step can be retried with the same id and gets the same answer.

| Phase | Who moves it | What can happen next |
| --- | --- | --- |
| queued | runner | working, stopped |
| working (planning, building, reviewing) | runner | needs_you, ready, failed, stopped |
| needs_you | runner; your reply moves it on | working, stopped |
| ready | runner, once checks pass | approved, discarded |
| approved | sous, after the runner accepts | published, or approved again on retry |
| published | sous, after the push and draft PR | cleaned |

**How sous runner calls map onto Orchid**

| sous call | Orchid side |
| --- | --- |
| `start` | queue the brief in the machine-wide pump (O7); refused until trust is acknowledged (O8) |
| `status` | read run state, progress and questions from Orchid's files (O2) |
| `reply` | `orchid answer <qid> <choice or text> --nonce <n>`, checked against the question generation (O6) |
| `stop` | `orchid run stop` (O5) |
| `approve` | `run accept`, then build and report the clean result branch (O3, O4) |
| `discard` | mark the result discarded, keep it until `clean` |
| `clean` | remove that run's worktree and result branch, never the shared pump |

## Milestones

The order now proves your actual goal first: a phone-triggered worker on your PC, using the runner that already exists. Orchid and headless planning improve the quality of the work after the loop is solid. Security comes in M1, not at the end.

| Milestone | You can… | sous | Orchid | Shellbell service | Shellbell mobile |
| --- | --- | --- | --- | --- | --- |
| **M0 Contracts v1** | nothing new for you yet; each repo can now build its side in parallel against published schemas and example files | S13, S14 | — | B2 (schema only) | — |
| **M1 Worker from the phone** | see the board, type a task, answer its questions, see the result; built-in Claude runner, allowed projects only | S1, S2, S4, S5, S6, S12 | — | B1, B2, B3, B5 | P1, P2, P3, P5, P8 |
| **M2 Approve and alerts** | get alerted when a run needs you or finishes, then approve a reviewed result as a draft PR | S7, S8, S9 | — | B4 | P4, P6 |
| **M3 Orchid as the worker** | send a bigger build to Orchid and watch it live: tasks, reviews, questions | S3, S10 | O1–O6, O8 | — | — |
| **M4 Headless planning** | a one-line brief becomes planned tasks with nobody at a keyboard | — | O9 | — | — |
| **M5 Unattended** | send ten tasks and go to bed | S11 | O7, O10, O11 | — | P7 |

**The M1 acceptance test:** start a task from the phone, restart the Shellbell service mid-submit, and confirm exactly one run exists. Then answer a question after reconnecting and confirm it reached the right question.

The IDs point to the change lists in the sections below.

## Changes by repository

Each repo below has its own list. A repo's work depends only on the contracts above, never on another repo's code, so the three lists can proceed in parallel once M0 is published.

| Repo | Owns | Implements | Changes | Starts at |
| --- | --- | --- | --- | --- |
| sous | the board, the front door, publishing; contracts C1 and C2 | C1 producer, C2 caller | S1–S14 | M0 |
| Orchid | the worker: queue, stop, acceptance, clean result | C2 runner, via `sous-runner-orchid` | O1–O11 | M3 (can start after M0) |
| Shellbell | the phone channel, board screen, alerts; contract C3 | C1 host, C3 both sides | B1–B5, P1–P8 | M0 |

### sous

sous is the index and the front door, not the scheduler. It already has `sous go --run -` (a background agent in its own worktree), `--key` to make a start safe to retry, `sous reply` with free text passed through, and `sous integration snapshot|watch --json`. Its own rule stays: retrying, reviewing and scheduling belong to a runner, never to sous.

| ID | Change | Why | Size | Milestone |
| --- | --- | --- | --- | --- |
| S1 | `run` and `stop` integration actions. `run` takes a project, a brief on stdin and a required key, so the action schema gains stdin-bound, typed parameters | The phone can only send actions that sous listed | M | M1 |
| S2 | **Allowed projects:** a per-project `remote_runs = true` setting, off by default. `run` refuses any project without it | A paired phone must not be able to start work in any repo on the machine | S | M1 |
| S3 | **Live plugin runners:** `watch` asks plugin runners for status every `poll_interval_ms`, within their time limit, and emits changes. Snapshot stays offline | Without it, Orchid runs freeze on the board (`internal/runs/runs.go:99`) | M | M3 |
| S4 | Run fields on snapshot items instead of new sections: `run.phase` (queued, planning, working, reviewing, needs_you, ready, done, failed), `run.progress`, `run.question` (id, generation, text, choices, other allowed), `run.result`. The `section` enum is unchanged, so Herdr's counts stay correct | New `section` values would quietly drop out of hosts that count only the known ones | M | M1 |
| S5 | `reply` carries the question id and generation the phone showed, and is refused if the run's current question differs | An answer typed before a reconnect must not answer a newer question | S | M1 |
| S6 | The board redesign: Needs you, In progress and Ready to approve groups, worked out from `run.phase`, plus a run view in `sous show` (sous UX) | You see at a glance what is moving | M | M1 |
| S7 | **Approve:** `sous approve <n>` calls the runner's optional `approve`, which records acceptance and returns a clean branch and the exact commit. sous checks the commit is the one you reviewed, pushes it, and opens a draft PR. On a retry it finds the existing PR by branch instead of opening a second | Publishing exactly what you saw, once | M | M2 |
| S8 | `sous discard <n>` calls the runner's optional `discard` and keeps the work until `clean` | Rejecting a result without losing it | S | M2 |
| S9 | A `write_public` effect for actions that publish, such as approve, so hosts know to confirm them | The current effects are only read, launch and write_private | S | M2 |
| S10 | `runner` as a per-project key | Big builds go to Orchid, quick fixes stay on the built-in runner | S | M3 |
| S11 | `sous doctor` checks for unattended use: allowed projects, runner set up, Orchid trust acknowledged, keep-awake available, agent credentials kept out | A failure at 2 a.m. should show up the day before | S | M5 |
| S12 | watch sends only changed items after the first snapshot, and takes --max-bytes: no line is ever larger, with long sections cut to "and N more" plus the count | Shellbell can pass each line through whole, under its 60 KB message cap, with no chunking of its own | M | M1 |
| S13 | C1 to v1: the additions above in integrations/schema.json, example files in integrations/examples/ covering the whole run lifecycle, and sous integration check for hosts | Shellbell and Herdr build and test against files, not a running sous | M | M0 |
| S14 | C2 to v1: a version number, docs/runner.schema.json, and sous runner check <path>, which runs the existing runnertest conformance suite against any runner program from the command line | Today the suite is an internal Go package, so a bash runner like Orchid's can only be checked from a sous checkout | M | M0 |

#### sous UX

The bar: you glance at any sous surface for two seconds and know what needs you, what is moving and what is ready, and you can act on each with one tap or one keystroke. The terminal, the Herdr pane, the menu bar and the phone all render the same snapshot in the same order, so learning one teaches the others.

```
┌──────────────────────────────────────────┐
│ sous                                     │
│ 1 needs you · 2 running · 1 ready · 21 on you
│                                          │
│ NEEDS YOU                                │   Questions first, answered in place:
│ ┌──────────────────────────────────────┐ │   choices as buttons; Reply… for free text
│ │ web · checkout redesign              │ │
│ │ Which fixture should win?            │ │
│ │ [Keep old] [Use new]  Reply…         │ │
│ └──────────────────────────────────────┘ │
│ IN PROGRESS                              │   Progress, not just "running":
│ app · add dark mode                      │   tasks done of total, and the current step
│ 3 of 7 tasks · reviewing T4 · 12m  ▰▰▰▱▱▱▱
│ api       · pairing timeout              │
│ planning · 2m                      ▰▱▱▱▱▱▱
│ 1 queued                                 │
│ READY TO APPROVE                         │   Reviewed, then approved:
│ ┌──────────────────────────────────────┐ │   approve pushes the branch, opens a draft PR
│ │ api  · retry note writes             │ │
│ │ 4 files · tests passed · view diff   │ │
│ │ [Approve] [Discard]                  │ │
│ └──────────────────────────────────────┘ │
│ (        + New task        )             │   One project, one brief, one tap:
└──────────────────────────────────────────┘   sent as sous go --run; a retry finds the same run
```

**Rules every surface follows**

1. **Order by urgency.** Needs you, then In progress, then Ready to approve, then on you, on others, unfinished. The first three groups come from each run's phase (S4), so every host shows them the same way. Ideas stay where they are now, shown in a project's own view.
2. **Every row has its next move.** In the terminal that's the exact command; on the phone it's a button. No row is just information.
3. **Plain words.** "Needs you", "3 of 7 tasks", "tests passed". Never Orchid's internal terms such as boundary, epoch or arbitrating.
4. **Instant, then live.** Show the cached board at once, then update in place from `watch`. Never a spinner over an empty screen. Say how old the data is when the computer is unreachable.
5. **Colour is never the only signal.** Each state also has a word or a symbol. sous's violet is the accent, used only where you need to act.
6. **Quiet when nothing needs you.** An empty Needs you section says so in one line, rather than showing an empty frame.
7. **Show evidence, not the agent's story.** A result card shows which checks ran and passed, and who reviewed it, not the agent's own summary. When review fell back to the same vendor, the card says so.

**The terminal board, after S8**

```
sous · 1 needs you · 2 running · 1 ready · 21 on you

  needs you
  91  web        which fixture should win?  1 keep old · 2 use new    sous reply 91 1

  in progress
  92  app        add dark mode          ▰▰▰▱▱▱▱  3/7 · reviewing T4    12m
  93  api        pairing timeout        ▰▱▱▱▱▱▱  planning              2m
      1 queued

  ready to approve
  88  api        retry note writes      4 files · tests passed    sous approve 88
```

**No web engine to build.** The phone board is a native Shellbell screen, the desktop board stays a terminal view, and both read the JSON sous already produces. I looked at [lavish-axi](https://github.com/kunchenguid/lavish-axi) as a ready-made web layer. It's an MIT-licensed reviewer for HTML files that an agent writes: you annotate the page, and the agent picks up your notes with a long poll. It doesn't host a live board. It runs through Node and npx, sends telemetry unless you turn it off, and reaches the phone only over Tailscale, which bypasses Shellbell's end-to-end channel. sous does not use it.

### Orchid

Orchid becomes the worker for bigger builds from M3 on. It already has evidence gates, a pump, git-backed recovery and questions with nonces. As the worker it also takes on what version 1 wrongly gave sous: the queue, cancellation, acceptance and a clean result. Its non-goals still hold: no server, dashboard or phone app.

The main structural change is that Orchid has one long-lived `orchid/integration` branch per repo, one active run at a time, and a rollover (`run new`) only from complete or blocked (`libexec/orchid-run:599-605`). Phone tasks therefore run **one Orchid run per repo at a time**, with the rest queued, and each finished run is published as its own clean branch.

| ID | Change | Why | Size | Milestone |
| --- | --- | --- | --- | --- |
| O1 | **`sous-runner-orchid`,** shipped in the Orchid repo: start, status, stop, reply, approve, discard, clean. Status runs within sous's time limit by reading Orchid's state files rather than chaining many commands | The bridge; it ships with the Orchid changes it depends on, and is done when sous runner check passes against it | M | M3 |
| O2 | Progress and the current question in `--json status`, plus `orchid --json questions` (id, generation, text, choices, nonce) | Feeds `run.progress` and `run.question` without parsing `BLOCKERS.md` | S | M3 |
| O3 | **Clean result branch:** at acceptance, build `orchid/result/<run>` from the project's base, containing the code changes only and no `.orchid/` state. Report the base commit, the result commit and the evidence | The integration branch is push-blocked by Orchid's own hook (`templates/pre-push.sh`) and holds private run state in its history | M | M3 |
| O4 | **Acceptance through the runner:** the runner's `approve` runs `run accept` with the reviewed result's evidence, then returns the clean branch and commit. A changed result refuses the approval | Today work stops at accepting, and nothing in version 1 called `run accept` | M | M3 |
| O5 | **Durable stop:** `orchid run stop` records a stopped state that every dispatcher honours, fences the epoch so late job results are rejected, kills jobs, and only then releases the lease | A released lease is treated as stale at once by the pump (`runners/orchid-pump:695`), so a stop that only kills jobs would restart | M | M3 |
| O6 | Choices plus an **Other** option on questions | Free text already works when a question has no choices; mixing both doesn't | S | M3 |
| O7 | **Machine-wide queue:** one Orchid pump for the machine that holds queued runs, one active run per repo, a `max_runs` cap and a daily spend or time cap. It reports `queued` through the runner | sous's rule puts scheduling in the runner; the pump is the part that runs things in the background | L | M5 |
| O8 | **Trust stays on the computer:** the runner refuses to start in a repo until `orchid trust unattended` has been acknowledged there. Starting the pump on first use never acknowledges it | The trust check exists so unattended running is a deliberate local choice | S | M3 |
| O9 | **Headless planning:** `orchid start --plan` runs the planning skill through an engine, then `plan apply`; a short brief may become one task. The verification command must be known before planning | Today `start` stops at planning, and the pump skips planning runs (`runners/orchid-pump:688-691`) | L | M4 |
| O10 | Keep-awake while jobs run: `caffeinate -w <pid>` around each engine job in `runners/orchid-launch` (macOS), `systemd-inhibit` on Linux | No privileged helper or protocol change needed | S | M5 |
| O11 | Visible stuck states: rate limited, sign-in expired, answer expired, stale lease. Each is reported as a phase with a reason | "Ten tasks at bedtime" can otherwise finish none of them silently | M | M5 |

### Shellbell

One repo, two parts. Both parts implement C3, and the service is a C1 host.

#### Service (computer)

The data channel carries only Shellbell's own message types today. Both sides drop messages they can't parse (`apps/agent/src/v2-phone-link.ts:80-84`), and features are exchanged in `hello`, so a board is a new feature on the existing end-to-end channel. Each inner message is capped at 60,000 bytes (`packages/protocol/src/session-v2-route-wire.ts:13`), and sous's `watch` currently sends the whole snapshot with each change, so sous will cap its own output (S12) instead of Shellbell splitting it. The request memory is lost on restart and covers only a fixed list of request types (`apps/agent/src/paired-requests.ts:4-33`). Shellbell must keep working with sous absent.

**Guardrails.** Shellbell works well today, so the design must bend to it, not the other way round:

1. **Off unless sous is there.** No sous on PATH means no board feature, no new messages, no new UI. The app looks exactly as it does today.
2. **No changes to terminal, session, conversation or pairing code.** The board lives in its own folders (`apps/agent/src/board/`, `apps/mobile/src/board/`, one new route) and touches existing code in three places only: one feature flag in `hello`, one entry in the control-message check, and one Board/Sessions switch.
3. **Reuse before adding.** App Lock covers board actions, and the existing alert reasons carry run alerts. No new native code, relay change or crypto.
4. **sous does the heavy lifting.** sous keeps every message small enough to send whole, so Shellbell never chunks, merges or rebuilds a board.
5. **Each piece can ship and be pulled back on its own.** If the board misbehaves, turning off the flag returns Shellbell to today's behaviour.

| ID | Change | Why | Size | Milestone |
| --- | --- | --- | --- | --- |
| B1 | **sous reader** in `apps/agent/src/board/`: when `sous` is on PATH, run `sous integration watch --json --max-bytes <n>` and relay each line as it comes. Restart it if it exits | sous already gives a ready-made board; Shellbell only passes it on. Tested against the C1 example files, with no sous installed | S | M1 |
| B2 | **`board-v1` flag** and pass-through messages in `inner.ts`: `board.update` (computer to phone, one sous watch line, sent whole), `board.action` and `board.result`. Regenerate `docs/protocol.md` | The smallest protocol addition that carries the board | S | M1 |
| B3 | **Actions:** check `board.action` against the latest `describe`, then run it with the phone's operation id as sous's `--key`. sous does the de-duplication on disk | Safe to retry without a new ledger in Shellbell | S | M1 |
| B4 | **Run alerts through the existing reasons:** a run that needs you raises `agent-blocked`, a finished run `agent-finished`, with `repository`, `title` and `agentName: sous` in the existing context and a session id of the form `sous:<n>`. The phone opens the board for `sous:` ids | No new reason, native receiver or relay change | S | M2 |
| B5 | Add `board.action` to the control messages in `apps/mobile/src/security/control-lock.ts`, so App Lock blocks it while locked; log each action locally | App Lock already exists and is on by default | S | M1 |

All of this is additive on the 1.x line. Shellbell's guide only asks for your approval for major or incompatible changes.

#### Mobile app

The app is Expo and React Native with expo-router. Screens are files under `app/`, and there are reusable `Card`, `Pill`, `Sheet` and `EmptyState` components in `src/ui/`. It already has one non-terminal view, for agent transcripts. A board screen is ordinary app work; most of the effort is in the connection and store plumbing.

| ID | Change | Why | Size | Milestone |
| --- | --- | --- | --- | --- |
| P1 | Board store in `apps/mobile/src/board/`: apply each `board.update` by revision, ask for a fresh snapshot when there's a gap, and keep the last board with its age when the computer is unreachable | Live and correct after a reconnect, never blank | M | M1 |
| P2 | **Board screen** `app/c/[fp]/board.tsx`: Needs you, In progress, Ready to approve, then the rest, plus a Board/Sessions switch. Boards are per computer, matching `app/c/[fp]/` | The mock-up above | M | M1 |
| P3 | **New task sheet:** allowed projects only (S2), recent first; a brief field; Start, covered by App Lock like any other control. It creates the operation id once and keeps the draft and the id until a `board.result` comes back, even across app restarts | "Type on my app and it triggers work", safely retried | M | M1 |
| P4 | **Result card:** checks and their outcome, reviewer and whether review was independent, files changed, and the diff loaded a page at a time. Approve asks for a confirm tap and names the exact commit | Approving what you actually saw | M | M2 |
| P5 | **Answer in place:** choice buttons, Other… for free text, sent with the question's id and generation | Answers land on the right question | S | M1 |
| P6 | Open the right card straight from a `run-needs-you` or `run-finished` alert | A question answered in seconds | S | M2 |
| P7 | Queue view: queued runs with their reasons (waiting for repo, rate limited), cancel | Managing ten tasks at bedtime | S | M5 |
| P8 | **Stop all** on the computer screen, which stops every run on that computer | One tap when something looks wrong | S | M1 |

Shellbell's mobile marketing version stays at 1.0.0 until the first public release, so these changes ship as new build numbers on that release, not a version bump.

## Security

A brief typed on the phone runs an agent with shell access on your computer, so this is remote code execution by design. Orchid says plainly that it has no filesystem or network sandbox, and a worktree limits nothing on its own. These come in M1, before the first remote run:

1. **Allowed projects only** (S2): remote runs are off for each project until you turn them on at the computer.
2. **Shellbell's App Lock** covers Start and Approve (B5): board actions count as control messages, so a locked phone can't send them. Plus a local audit log of every remote action.
3. **No credentials in the agent's environment:** no `gh` or `glab` token, no SSH agent, no cloud keys. Only sous holds publishing credentials, and only uses them at Approve.
4. **Stop all** on the phone (P8), and revoking a lost phone through Shellbell's existing device revocation.
5. **Briefs are untrusted input** to an agent with network access. The agent's permission profile is the real boundary, so it is a decision below.

Cost is a separate limit: counting runs doesn't bound one expensive run. Each run needs a time or spend ceiling, and the machine needs a daily one (O7).

## Decisions only you can make

Each of these changes what gets built. My recommendation is in each one; all four reviewers agreed with the first three.

- [ ] **Order.** Ship the phone loop on the built-in Claude runner first (M1–M2), bring Orchid in at M3, and build headless planning at M4.
- [ ] **Who schedules.** Orchid's machine-wide pump owns the queue and caps; sous only shows `queued`. One reviewer preferred sous keeping an overall cap across all runners, which would mean changing sous's own rule; I'd keep the rule.
- [ ] **Publishing.** Runs never push. Approve pushes a clean per-run branch tied to the commit you reviewed, as a draft PR.
- [ ] **Agent sandbox.** Run unattended agents with no credentials, write access only to their worktree, and network access. A macOS `sandbox-exec` profile or a container would make the worktree limit real; a container is stronger but slower to set up.
- [ ] **What "ready" means per project.** A run can reach Ready to approve only if the project has a check command and it passed.
- [ ] **Telegram questions.** For repos that sous starts, turn off Orchid's own Telegram or WhatsApp channel so questions have one route and one nonce.
- [ ] **Several computers.** Boards stay per computer, and you pick the computer when you start a task. A merged board across machines is out of scope for now.

**Where to start:** M0 in sous: S13 and S14, which publish C1 v1 and C2 v1 with their example files and check commands, plus the C3 message definitions in Shellbell. After that, sous (S1, S2, S4) and Shellbell (B1–B3, P1–P3) work in parallel towards the M1 acceptance test.
