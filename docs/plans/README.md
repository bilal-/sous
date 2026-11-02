# Planning archive

These documents record the phone-build proposal from October 2026 and the
delivery of its sous changes. They preserve the reasoning and proposed work
across sous, Shellbell and Orchid. They are not current command or protocol
specifications, and an unchecked proposal does not establish unfinished work
in another repository.

For current behavior, read [the design](../design.md),
[commands](../commands.md), [host integrations](../integrations.md) and
[plugin contracts](../plugins.md). The design and the pinned AXI policy in
[AGENTS.md](../../AGENTS.md#axi) remain authoritative. The proposals do not
give sous responsibility for scheduling, reviewing or retrying work; those
belong to runners.

| Document | What it records |
| --- | --- |
| [Autonomous build from the phone](autonomous-build.md) | The proposed product split, milestones and work in each repository. Source line numbers and descriptions of the other repositories reflect the draft date. |
| [Protocols v1 draft](protocols-v1-draft.md) | The original contract proposal, including alternatives that were changed or dropped before implementation. Its examples must not be used as current wire fixtures. |
| [Sous delivery record](sous-goal.md) | The seven sous rounds released as v0.13.0 through v0.19.0, followed by the v0.20.0 browser board and a separate Shellbell prototype. |

## What changed after the proposals

Checked against sous main on 2026-11-01:

| Proposal | Current contract |
| --- | --- |
| Replace the host schema with v1 | Version 0 remains in `integrations/schema.json`; version 1 is beside it in `integrations/schema.v1.json`. Hosts select it explicitly with `--protocol 1`. |
| Runner requests use `v: 1` | Runner JSON still uses `v: 0`. A successful `describe` advertises supported calls and fields. |
| Questions use an integer generation | Replies name the question's `id` and opaque `token`. |
| Operation receipts expire after 30 days | Receipts are retained without expiry. A process lock protects an active request; `in_progress` uses exit code 5. Browser done and snooze operations also retain retry identity. |
| Add `sous integration check` | This command was dropped. Schema and example validation cover the host contract; `sous runner check` checks runner programs. |
| Keep the desktop board terminal-only | `sous serve` has provided a local browser board since v0.20.0. |
| Strip credentials to isolate an agent | Credential filtering reduces inherited credentials; it is not a filesystem or network sandbox. Runner-reported checks and reviews are not independently verified by sous. |
| Release after every development round | That described the original delivery sequence. Current work lands on `main` with `make ci` passing; publishing a release is a separate decision under [the release guide](../../AGENTS.md#releasing). |

The Shellbell and Orchid milestones describe work owned by those repositories.
Shipping the sous contracts does not prove that a phone interface, runner
bridge or device qualification has shipped. Check the owning repository and
its release evidence before treating those milestones as complete.
