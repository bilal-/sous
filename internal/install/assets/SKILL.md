---
name: sous
description: Use automatically on session startup, when restoring context across projects, before asking what was decided or where work left off, whenever a durable decision, constraint, lesson, or follow-up comes up, when wrapping up work (updating repo STATUS.md/FOLLOWUPS.md when present else saving a private handoff in sous), or when handing a task to a background agent.
---

# sous

`sous` is the user's automatic cross-repo working memory and to-do board. Run `sous help` first: it lists every command and all rules for agents, and every command supports `--help`.

Workflow:
- **Start:** Read any `sous` briefing injected by session hooks (or run `sous here`), plus any repo `STATUS.md` or `FOLLOWUPS.md` listed there.
- **Recall before asking:** Search past context across memories, agent memories, `STATUS.md`, `FOLLOWUPS.md`, open notes, and session history with `sous recall`.
- **Capture proactively:** Record lasting decisions, constraints, lessons, and questions with `sous remember`, and open to-dos or postponed work with `sous note` when appropriate.
- **Wrap up automatically:** Prefer updating `STATUS.md` and `FOLLOWUPS.md` when the repo already uses them (never create them unasked); otherwise save a concise private handoff with `sous remember`.
