# MemoryCore experiment

The first local pass gives no evidence that adopting MemoryCore improves recall
over searching saved handoffs. Both retrievers passed 9 of 11 small fixture
cases. MemoryCore ranked an outdated decision ahead of its replacement, and
replaying an ingestion created another copy. Keep it an optional candidate
for future retrieval research. Sous now implements sourced memory, explicit
sharing and cross-project efforts locally; see the
[command guide](../../docs/commands.md#memory-and-efforts). These results describe
the isolated fixture experiment, not a benchmark of those production commands.

This is a standalone experiment, not a sous command or plugin. It changes no
session hooks, personal state, agent configuration, or production dependencies.
The gateway runs only during the experiment and is stopped afterward.

## What was run

The source is pinned to TencentDB-Agent-Memory commit
[`ecc01a545da0b2ac6cae2f0f0af0d39d41469793`](https://github.com/TencentCloud/TencentDB-Agent-Memory/tree/ecc01a545da0b2ac6cae2f0f0af0d39d41469793).
`prepare.py` verifies the source archive checksum. `runtime-package-lock.json`
pins the experimental Node dependencies separately from sous's Go module.

The corpus has ten fictional handoffs from three projects in two workspaces,
plus a separate snapshot of current commitments. It includes a reversed
pagination decision, a reusable retry fix, a personal canary, and an agent's
unverified claim that a release is finished.

The run uses MemoryCore's real HTTP API and SQLite FTS5 search. Automatic
extraction and embeddings are deferred at the user's request: no model
credentials are loaded, the model URL is an unused loopback address, and
the worker reports zero extraction tasks. Nothing reads real agent sessions
or calls an agent CLI.

The baseline searches the same original handoffs using unique keyword overlap,
after filtering by workspace and project. Newer records break ties. This is a
transparent baseline, not a claim to implement sous's existing search or BM25.
MemoryCore searches each selected project separately; the adapter merges the
returned scores and keeps three records. Scores across project searches may
not be calibrated, which remains an integration question.

## Observations

| Check | Local result |
| --- | --- |
| Required sources present in the top three, or correctly empty | Baseline 9/11; MemoryCore 9/11 |
| Two vocabulary-mismatch queries | Both missed both: no evidence of semantic recall in this mode |
| Current pagination decision ranked first | Baseline yes; MemoryCore no |
| Superseded decision also returned | Both returned it; neither resolved the contradiction |
| Personal canary retrieved inside work scope | No, for either retriever |
| Personal canary retrieved inside its own scope | Yes, for both |
| Same conversation ingested twice | MemoryCore's session count increased from 1 to 2 |
| Gateway stopped before another recall | Adapter retained the previous three results and marked them stale/unavailable |
| Current-work snapshot combined with historical context | Four open items across two work projects; completed and personal items excluded |
| Agent says a release is done | The authoritative release smoke-check item remained open |

The first nine retrieval cases were defined before querying either retriever.
After that smoke run, two paraphrase stress cases were added, with expected
sources defined before querying either retriever on those additions. This is
a small exploratory fixture set, not a held-out benchmark or a model-quality
evaluation. A retrieval pass only means the required evidence appeared; it
does not mean the system understood it or gave a correct answer.

For summary isolation, `probe-scope.mjs` executes upstream's actual
`buildProfileIsolationScope` function. Different task IDs with the same team
and agent produce the same scope. Different project-agent IDs or workspace-team
IDs produce different scopes. **This is an executable scope-function check,
not an end-to-end test of generated summaries.** It confirms that mapping
projects only to tasks would be inadequate for sous.

The adapter's proposed mapping is:

| Sous concept | Experimental MemoryCore scope |
| --- | --- |
| Workspace | Stable `team_id` |
| Project within that workspace | Stable logical `agent_id` |
| Person | `user_id` |
| Session | `session_id`, retained on the original record |
| Actual agent tool | Source metadata; changing tools does not change project scope |

The work brief demonstrates the other half of sous: organizing commitments
across projects. Current work state determines which items are open and which
section they occupy. Historical memory supplies context. It cannot close,
reclassify, or invent commitments. This prototype uses a fixture snapshot;
connecting it to sous's real board is not part of this pass.

## Integration findings

* The pinned search API returns `data.messages`; its API guide describes
  `data.items`. The adapter checks the observed response and treats a changed
  shape as unavailable, rather than as an empty result.
* Setting `memory.extraction.enabled` to false alone did not prevent the
  conversation endpoint from scheduling extraction in an initial smoke run.
  The final run also disables pipeline warmup and sets thresholds beyond this
  bounded experiment. Its final health response verifies zero consumed tasks.
  This is not a proposed permanent production disable mechanism.
* `/v3/scenario/write` updates existing summaries in this revision; a write
  to a nonexistent summary returned 404. Since extraction is deferred, the
  summary check uses the upstream scope function instead of fabricating
  generated summaries.
* Source IDs are tagged in the controlled handoff text and resolved against the
  local corpus. Production ingestion needs durable receipts linking upstream
  record IDs to original sources, and a retry policy that prevents duplicates.
* The fixture uses synthetic scope IDs directly in the data-plane API. The
  gateway warns that these agents are absent from its metadata registry.
  Team provisioning, asset bindings, authorization, and multi-user access
  control have not been tested.
* Search latency is recorded per request, but this corpus is too small for a
  performance conclusion. No LLM extraction, embedding search, automatic
  conflict resolution, deletion propagation, or production-scale behavior
  was measured.

The useful next product work is a history of handoffs and decisions with
sources, dates, explicit supersession, and workspace boundaries, connected to
the current board. MemoryCore remains a candidate for optional extraction and
retrieval once those contracts are clear and a separate live-model experiment
is authorized.

## Reproduce

Offline regression tests require only Python 3.9+ and run with
`make test-experiments`. They are also included in `make test` and CI.

The full experiment requires Python 3.9+, Node.js 22.16+ with SQLite FTS5, and npm. Installation
needs internet access; the experiment itself uses loopback connections only.
Use a new destination and output directory each time. The runtime argument
must be the source produced by `prepare.py`; do not point it at an edited
upstream checkout when comparing the saved results.

```sh
make test-experiments

python3 experiments/memory-core/prepare.py \
  --destination /tmp/acme-memory-source

python3 experiments/memory-core/experiment.py \
  --source /tmp/acme-memory-source/TencentDB-Agent-Memory-ecc01a545da0b2ac6cae2f0f0af0d39d41469793 \
  --output /tmp/acme-memory-results
```

The source download is about 36 MB; the tested runtime dependencies used about
450 MB on macOS arm64. Installation removes the upstream package's agent peer
dependencies and optional cloud integrations, then uses `npm ci` with lifecycle
scripts disabled. Its home and npm cache are under the disposable destination.
Runtime data and home directories are temporary and are removed on exit. The
source installation and output directory remain available for inspection and
can be removed when finished.

The program exits zero when the measurements completed, even when a retrieval
case failed. Inspect `summary`, individual query results, and `not_measured`
before drawing conclusions. Infrastructure failure produces `completed: false`
and a nonzero exit; unavailable memory is never counted as an empty answer.

Saved evidence from the clean, locked installation:

* [Results](results/results.json), including exact source and dependency pins,
  query outcomes, retry counts, scope probe, and gateway health before/after.
* [Work brief](results/work-brief.json), combining fictional current commitments
  and source-linked context.
* `gateway.log` beside the results, retained locally and ignored by Git.

Adapter tests use fake responses and fixture data. They cover unavailable and
malformed memory, retaining prior context, partial failure, workspace-safe
cache fallback, source validation, unchanged commitments, unsupported fixture
versions, and archive verification. The suite was written failure-first.

Validation for the recorded pass: all 14 experiment checks passed; `make test`
passed with the installed Go 1.27.3; `GOTOOLCHAIN=go1.27.1 make ci` passed with
the version declared by `go.mod`. The system-toolchain `make ci` attempt hit a
staticcheck export-format incompatibility before tests, so the successful CI
run used the repository's declared compiler without changing global tools.
