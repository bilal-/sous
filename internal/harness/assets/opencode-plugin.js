// sous: written by sous setup; it is replaced when sous is set up again.
// It tells the agent where the person left off when a session starts, and
// records what the agent said last when the session goes idle.
import { spawnSync } from "node:child_process"

// sous hook session-start: {{start}}
// sous hook session-end: {{end}}
const start = {{startJSON}}
const end = {{endJSON}}

// sous runs as a hook would: JSON on standard input, its answer on
// standard output, a few seconds at most, never in the agent's way.
function sous(command, input) {
  try {
    const r = spawnSync("sh", ["-c", command], { input: JSON.stringify(input), encoding: "utf8", timeout: 6000 })
    return r.status === 0 ? r.stdout : ""
  } catch {
    return ""
  }
}

export const Sous = async ({ directory }) => {
  const told = new Map() // session → what sous said at its start
  const assistant = new Set() // ids of the assistant's messages
  const last = new Map() // session → the assistant's last text
  return {
    "experimental.chat.system.transform": async (input, output) => {
      const id = input.sessionID || ""
      if (!told.has(id)) told.set(id, sous(start, { session_id: id, cwd: directory, source: "startup" }).trim())
      if (told.get(id)) output.system.push(told.get(id))
    },
    event: async ({ event }) => {
      const p = event.properties || {}
      if (event.type === "message.updated" && p.info && p.info.role === "assistant") assistant.add(p.info.id)
      if (event.type === "message.part.updated" && p.part && p.part.type === "text" && assistant.has(p.part.messageID) && p.part.text) {
        last.set(p.part.sessionID, p.part.text)
      }
      if (event.type === "session.idle" && p.sessionID) {
        sous(end, { session_id: p.sessionID, cwd: directory, last_message: last.get(p.sessionID) || "" })
      }
    },
  }
}
