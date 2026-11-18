// sous: written by sous setup; it is replaced when sous is set up again.
// It tells the agent where the person left off when a session starts, and
// records the session's prompt and last reply each time the session goes idle.
import { spawn, spawnSync } from "node:child_process"

// sous hook session-start: {{start}}
// sous hook session-end: {{end}}
const start = {{startJSON}}
const end = {{endJSON}}
const timeout = {{timeoutMs}}

export const Sous = async ({ directory }) => {
  const told = new Map() // session → what sous said at its start
  const roles = new Map() // message → { session, role }
  const last = new Map() // session → the assistant's last text
  const prompts = new Map() // session → the user's latest prompt text
  const forget = (session) => {
    if (!session) return
    for (const [id, meta] of roles) {
      if (meta.session === session) roles.delete(id)
    }
  }
  const sendEnd = (session) => {
    if (!session) return
    tell(end, {
      session_id: session,
      cwd: directory,
      prompt: prompts.get(session) || "",
      last_message: last.get(session) || "",
    })
    forget(session)
  }
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
      if (event.type === "message.updated" && p.info && (p.info.role === "assistant" || p.info.role === "user")) {
        const prev = roles.get(p.info.id)
        roles.set(p.info.id, { session: p.info.sessionID || (prev && prev.session), role: p.info.role })
      }
      if (event.type === "message.part.updated" && p.part && roles.has(p.part.messageID)) {
        const meta = roles.get(p.part.messageID)
        const session = p.part.sessionID || meta.session
        roles.set(p.part.messageID, { session, role: meta.role })
        if (p.part.type === "text" && p.part.text && session) {
          if (meta.role === "user") prompts.set(session, p.part.text)
          else last.set(session, p.part.text)
        }
      }
      // The end is told and not waited for: opencode carries on at once.
      if ((event.type === "session.idle" || event.type === "session.compacted") && p.sessionID) {
        sendEnd(p.sessionID)
      }
      if (event.type === "session.deleted" && p.info) {
        told.delete(p.info.id)
        last.delete(p.info.id)
        prompts.delete(p.info.id)
        forget(p.info.id)
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
    child.stdin.on("error", () => {})
    child.stdin.end(JSON.stringify(input))
    child.unref()
  } catch {}
}
