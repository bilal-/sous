# Using sous with your agent

sous is a plain command, so any agent that can run shell commands can use
it. What differs is how much `sous setup` does for you.

## Claude Code

`sous setup` adds four hooks (`SessionStart`, `Stop`, `PreCompact` and
`SessionEnd`). When a session starts in a project, Claude sees where you left
off; turn stops, compaction and session end record progress (branch, prompt,
touched files and last message) automatically. The `sous` skill tells Claude
that sous is there and when to use it, and `sous help` teaches it the rest. So
you can say things like:

* "what's waiting on me?"
* "note for billing that I owe Sam the pricing copy"
* "where did I leave off here?"
* "have an agent fix the flaky test in billing"

Claude asks before anything reaches a shared tracker.

## Codex

The same skill, plus `SessionStart`, `Stop` and `PreCompact` hooks by default.
Run `sous setup --codex-session-end` to add the optional `SessionEnd` hook on
versions that support it. Setup adds descriptive `statusMessage` labels to
Codex's hooks so `/hooks` shows sous's purpose instead of `Unnamed hook`.
Codex makes you review and trust new or changed hooks through `/hooks` before
they run. Setup installs the hooks and `sous doctor` checks they are there, but
neither can trust them for you. See
[Codex's hook documentation](https://developers.openai.com/codex/hooks).

Once hooks run, `sous here` shows your recent Codex work automatically.
Without them, `sous here` still shows the last commit and your notes.

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

## Qwen Code, Gemini CLI and pi

* **Qwen Code:** `sous setup` installs `SessionStart`, `Stop`, `PreCompact` and
  `SessionEnd` in `~/.qwen/settings.json` and the skill in `~/.qwen/skills`.
  `sous go --run -a qwen` runs headless.
* **Gemini CLI:** `sous setup` installs `SessionStart` (injecting JSON
  `hookSpecificOutput.additionalContext`), `AfterAgent`, `PreCompress` and
  `SessionEnd` in `~/.gemini/settings.json` and the skill in `~/.gemini/skills`.
  `sous go --run -a gemini` runs headless.
* **pi:** `sous setup` writes `~/.pi/agent/extensions/sous.ts` (`session_start`,
  `before_agent_start`, `agent_end`, `session_before_compact`,
  `session_shutdown`) and the skill in `~/.pi/agent/skills`.
  `sous go --run -a pi` runs headless.

## Copilot CLI, Cursor, Factory Droid and Kimi Code

`sous setup` wires hooks automatically when each tool is installed:

* **Copilot CLI:** `~/.copilot/hooks/sous.json` (`sessionStart`, `sessionEnd`).
* **Cursor:** `~/.cursor/hooks.json` (`sessionStart`, `stop`, `sessionEnd`).
* **Factory Droid:** `~/.factory/hooks.json` (`SessionStart`, `Stop`,
  `PreCompact`, `SessionEnd`).
* **Kimi Code:** `[[hooks]]` in `~/.kimi-code/config.toml` (`UserPromptSubmit`
  injecting startup context once per session, plus `Stop` and `SessionEnd`).

## Other agents

Many agents read skills from a shared folder, `~/.agents/skills`, and
`sous setup` puts the sous skill there. Agents without built in hook support
(such as Amp, Goose or Cline) won't get automatic session-start injection yet.
For those, add a line to the agent's instructions file (such as `AGENTS.md`)
asking it to run `sous here --brief` when a session starts.

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
