# The board's design: tokens and display rules

sous sends the same JSON everywhere (the [native integration
protocol](../../docs/integrations.md), version 1). Each surface draws it with
its own components: the web page `sous serve` shows on the computer, and
Shellbell's screen on the phone. What makes them one product is not shared
code but these two things:

* [`tokens.json`](tokens.json): colours (dark and light), the colour and
  word for each group, type, corner radii and spacing.
* The display rules below. A surface that follows them shows the same
  board, in the same order, in the same words.

## Display rules

1. **Order by what the person does next.** Groups, top to bottom:
   needs you, in progress, ready to review, then on you, on others,
   unfinished. Ideas and snoozed items belong in a project's own view,
   not on the board.
2. **Group runs by phase, not by section.** An item with a `run` goes in a
   group from `run.phase`, falling back to `run.state` when there is no
   phase:

   | `run.phase` | `run.state` | Group |
   |---|---|---|
   | `needs_you` | `needs_you` | needs you |
   | `queued`, `planning`, `working`, `reviewing`, `blocked` | `starting`, `running` | in progress |
   | `ready`, `approved` | `done` | ready to review |
   | `published` | | ready to review, showing the pull request |
   | `failed`, `stopped`, `discarded` | `failed` | its `section` (on you) |
   | anything else | | in progress, with the phase as it is written |

   An item without a run goes in its `section`.
3. **Plain words.** Each phase reads as one word: queued, planning,
   working, reviewing, blocked (with `run.reason`), ready, approved,
   published, failed, stopped, discarded. Never a runner's own terms.
   `run.progress` reads "3 of 7", and its `step` replaces the phase word
   when there is one.
4. **Every row has its next move**, drawn from the item's `actions`: a
   question's choices as buttons (and a text box when `other` is true),
   Approve and Discard on a result, Stop on a run at work. Show only the
   actions an item lists.
5. **Approve shows what it publishes.** Before Approve can be pressed the
   surface shows the result's commit (12 characters), its files, each
   check with its status, and who reviewed it, saying "same vendor" when
   `review.independent` is false. Approve asks once more, naming the
   commit, and sends that commit. Checks and review are the runner's word:
   label them as reported.
6. **Missing is never zero.** When the snapshot has `problems`, or a run
   has `error`, say so where the counts are, as the terminal board's `?`
   does. A count is left out only when it is truly zero and nothing is
   missing. `omitted` counts items a surface was not sent.
7. **Instant, then live.** Show the last board at once, then update in
   place from `watch`. When the computer cannot be reached, keep the board
   and say how old it is.
8. **Colour is never the only signal.** Each group has its word, and
   colour only repeats it.
9. **Every action is an op.** Make one op id per thing the person asked
   for, keep it until an outcome comes back, and send the same op again
   after a reconnect. Show `already_done` as done; show `conflict` by
   reloading the item and saying what changed.
