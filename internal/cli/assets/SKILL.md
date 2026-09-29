---
name: sous
description: Use when the user asks what is waiting on them or where they left off, says "note this", "remind me", "later", "follow up", "file this", or "sous", or when work is being put off (a TODO for another day, something to check with a person, an idea for another project). sous is the user's personal list of what is waiting on them across every project; it never does work itself.
---

# sous

sous keeps one list of what is waiting on the user, across every project they
work on. It holds short notes and pointers. The work itself stays in git, in
the project's tracker, or in its `FOLLOWUPS.md`. You use it by running the
`sous` command. It needs no API key.

## Reading

* `sous` shows the board: everything waiting on the user, in every project.
* `sous here` shows where the user left off in the current project. It is
  added to your context when a new session starts. After `/clear`, a resume,
  or a compaction, run it yourself.
* `sous <project>` does the same for a project by name.
  `sous projects [name]` lists the projects sous can see.
* Every reading command takes `--json`.

## Writing notes

* `sous note "text"` saves an idea for the current project and prints its
  number.
* `sous note -k me "text"` is something the user has to do.
  `-k them` means the user is waiting on someone else.
* `sous note -p <project> "text"` saves a note for another project, from
  anywhere. The name can be rough: sous tries the exact name, then the start
  of a name, then any part of it.
* `sous edit <n> "text"`, `sous kind <n> me|them|idea`,
  `sous snooze <n> [days]` and `sous done <n>` change or close a note.

## Writing to a shared tracker

Three commands write where other people can see: `sous file <n>`,
`sous note --file ...` and `sous done <n> --close`. They reach the project's
tracker, such as its `FOLLOWUPS.md`, GitHub issues or GitLab issues.

Before running any of them, **ask the user, end your turn, and run it only
after they say yes in their next message.** Asking and running in the same
turn is not consent.

## Reading `sous here`

* A note with `→ github:acme/api#12` (or `md:`, `gitlab:`) was filed in that
  tracker.
* `(ref missing)` means the tracker no longer has the item.
  `(status unavailable: ...)` means sous could not ask it. Neither means the
  work is done. Tell the user rather than closing anything.
* A line starting with `✓` and `(closed upstream ...)` is a note that was
  closed in its tracker. It is shown so the user knows it happened.

## When to offer a note

Offer, in one short line, to note something with sous when:

* the user decides to do something later ("let's do that tomorrow")
* you stop with work unfinished, or leave a TODO
* the user is waiting on a person (a review, an answer, a sign off)
* an idea comes up that belongs to another project

Ask first. Write it only when they say yes, or when they asked you to keep
track of such things.

## Rules

1. Notes stay private to the user unless they ask you to file one.
2. If a project name matches more than one project, sous stops and lists
   them. Show the list to the user and ask. Never pick one yourself.
3. When you write a note on your own initiative, set `SOUS_SOURCE=agent` so
   the user can tell your notes from theirs.
4. Never edit the `<!-- sous:... -->` markers in `FOLLOWUPS.md` or in
   issues by hand. To close an item, tick its box or use `sous done`.
5. Never create `FOLLOWUPS.md`, change `~/.sous/config.toml`, or set up a
   tracker just to make `sous file` work. If sous says there is no tracker,
   tell the user and stop.
6. `done` means the user's own task is finished. You finishing some code is
   not the same thing, so do not close their notes unless they say so.
7. sous never does work. If the user asks sous to "go fix" something, that
   job is yours. Only record it with `sous note -k me` if the user wants it
   kept on their list.
8. Some agents run commands in a sandbox where reading and writing
   `~/.sous/` are separate permissions. If a write is refused, tell the user
   instead of trying again.
