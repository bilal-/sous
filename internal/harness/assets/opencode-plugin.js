// sous: written by sous setup; it is replaced when sous is set up again.
// It tells the agent where the person left off when a session starts, and
// records what the agent said last each time the session goes idle.
import { spawn, spawnSync } from "node:child_process"

// sous hook session-start: {{start}}
// sous hook session-end: {{end}}
const start = {{startJSON}}
const end = {{endJSON}}
const timeout = {{timeoutMs}}

export const Sous = async ({ directory }) => {
  const told = new Map() // session → what sous said at its start
  const assistant = new Set() // ids of the assistant's messages
  const last = new Map() // session → the assistant's last text
  return {
    // The start is asked for once per session, before its first model
    // call, and waited for: it is what the agent is told. A session that
    // already has the assistant's words in it is one carried on, not new.
    "experimental.chat.system.transform": async (input, output) => {
      const id = input.sessionID
      if (!id) return
      if (!told.has(id)) {
        const source = last.has(id) ? "resume" : "startup"
        told.set(id, ask(start, { session_id: id, cwd: directory, source }))
      }
      if (told.get(id)) output.system.push(told.get(id))
    },
    event: async ({ event }) => {
      const p = event.properties || {}
      if (event.type === "message.updated" && p.info && p.info.role === "assistant") assistant.add(p.info.id)
      if (event.type === "message.part.updated" && p.part && p.part.type === "text" && assistant.has(p.part.messageID) && p.part.text) {
        last.set(p.part.sessionID, p.part.text)
      }
      // The end is told and not waited for: opencode carries on at once.
      if (event.type === "session.idle" && p.sessionID) {
        tell(end, { session_id: p.sessionID, cwd: directory, last_message: last.get(p.sessionID) || "" })
        assistant.clear()
      }
      if (event.type === "session.deleted" && p.info) {
        told.delete(p.info.id)
        last.delete(p.info.id)
      }
    },
  }
}

// ask runs a sous hook and waits, at most timeout, for its answer: JSON on
// standard input, the answer on standard output; "" when there is none.
function ask(command, input) {
  try {
    const r = spawnSync("sh", ["-c", command], { input: JSON.stringify(input), encoding: "utf8", timeout })
    return r.status === 0 ? r.stdout.trim() : ""
  } catch {
    return ""
  }
}

// tell runs a sous hook without waiting for it.
function tell(command, input) {
  try {
    const child = spawn("sh", ["-c", command], { stdio: ["pipe", "ignore", "ignore"], detached: true })
    child.on("error", () => {})
    child.stdin.end(JSON.stringify(input))
    child.unref()
  } catch {}
}
