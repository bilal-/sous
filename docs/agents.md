# Using sous with your agent

sous is a plain command, so any agent that can run shell commands can use
it. What differs is how much `sous setup` does for you.

## Claude Code

`sous setup` adds two hooks. When a session starts in a project, the agent
sees where you left off. When it ends, sous keeps the session's last message
for next time. The `sous` skill tells Claude that sous is there and when to
use it, and `sous help` teaches it the rest. So you can say things like:

* "what's waiting on me?"
* "note for billing that I owe Sam the pricing copy"
* "where did I leave off here?"
* "have an agent fix the flaky test in billing"

Claude asks before anything reaches a shared tracker.

## Codex

The same skill, and a hook when a session starts. Run
`sous setup --codex-session-end` to add the optional end hook on versions
that support it. Codex makes you review and trust new or changed hooks
through `/hooks` before they run. Setup installs the hooks and `sous doctor`
checks they are there, but neither can trust them for you. See
[Codex's hook documentation](https://developers.openai.com/codex/hooks).

Once the end hook runs, `sous here` shows how your last Codex session ended.
Without it, `sous here` still shows the last commit and your notes.

## Antigravity

The IDE skill goes in `~/.gemini/config/skills`, and hooks in
`~/.gemini/config/hooks.json`, which both the Antigravity app and the `agy`
command read. A new conversation starts knowing where you left off, and the
end of each turn is recorded, so `sous here` shows how your last Antigravity
session ended.

The CLI's own skill folder, `~/.gemini/antigravity-cli/skills`, isn't filled
by setup yet. To add the skill there:

    mkdir -p ~/.gemini/antigravity-cli/skills/sous
    sous setup --print-skill > ~/.gemini/antigravity-cli/skills/sous/SKILL.md

See [Antigravity's skill locations](https://www.antigravity.google/docs/skills).

`sous go --run -a agy` hands a task to Antigravity in the background. A run
has the command permissions you gave Antigravity in its settings and no
more. If it tries a command it isn't allowed, the run shows as needing you
and names what was refused.

## opencode

`sous setup` writes a small plugin at `~/.config/opencode/plugins/sous.js`
using opencode's callbacks. It shows where you left off when a session
starts, and records the agent's last message when the session goes idle.
The skill comes from `~/.agents/skills`. `sous go --run -a opencode` hands it
a task in the background, with the permissions your opencode config gives.

## Gemini CLI, Kimi, Cursor and others

Many agents read skills from a shared folder, `~/.agents/skills`, and
`sous setup` puts the sous skill there. These agents then know sous is there
and when to use it. They don't get the summary at the start of a session
yet. For that, add a line to the agent's instructions file (such as
`AGENTS.md` or `GEMINI.md`) asking it to run `sous here --brief` when a
session starts.

For an agent that reads no skills folder, `sous setup --print-skill` prints
the skill so you can paste it into its instructions.

If you get sous working well with another agent, please share how in an
issue, or send a pull request adding hooks for it.

## Built for people and agents

The same commands serve you in a terminal and an agent in a session. sous
follows the [AXI](https://axi.md) guidelines (as of
[this version](https://github.com/kunchenguid/axi/blob/e15f82dd8e75ff640aaefd5c76c49471c5bcbbea/docs/index.html))
for agent friendly command line tools, where they help people too:

* running `sous` with nothing else shows live data, not help
* counts come first, and empty results say so plainly
* output ends with the next command you're likely to want
* errors are clear, nothing ever waits for input, and unknown flags fail
* the agent sees your context when a session starts, and a tiny skill tells
  it sous is there

The command line is the one source of truth. The skill only says when to
reach for sous and to run `sous help`, which lists every command and the
rules for agents (such as asking before anything reaches a shared tracker).
So the skill never needs updating when sous changes.

Output is plain text for people, with `--json` for programs.
