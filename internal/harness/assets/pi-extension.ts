// sous: written by sous setup; it is replaced when sous is set up again.
// It injects where the user left off on a new pi session, and records
// prompt + final response at turn end, compaction, and shutdown.
import { spawn, spawnSync } from "node:child_process"

// sous hook session-start: {{start}}
// sous hook session-end: {{end}}
const start = {{startJSON}}
const end = {{endJSON}}
const timeout = {{timeoutMs}}

function extractText(content) {
  if (typeof content === "string") return content
  if (!Array.isArray(content)) return ""
  const out = []
  for (const part of content) {
    if (part && part.type === "text" && typeof part.text === "string" && part.text) {
      out.push(part.text)
    }
  }
  return out.join(" ")
}

function fromMessages(messages) {
  let prompt = ""
  let last = ""
  if (!Array.isArray(messages)) return { prompt, last }
  for (let i = messages.length - 1; i >= 0; i--) {
    const raw = messages[i]
    const msg = raw && raw.message ? raw.message : raw
    if (!msg || !msg.role) continue
    if (!last && msg.role === "assistant") {
      const txt = extractText(msg.content)
      if (txt) last = txt
    }
    if (!prompt && msg.role === "user") {
      const txt = extractText(msg.content)
      if (txt) prompt = txt
    }
    if (prompt && last) break
  }
  return { prompt, last }
}

export default function (pi) {
  const told = new Map() // session -> context string
  const injected = new Set() // session ids already injected

  const sessionMeta = (ctx) => ({
    id: (ctx && ctx.sessionManager && ctx.sessionManager.getSessionId && ctx.sessionManager.getSessionId()) || "",
    file: (ctx && ctx.sessionManager && ctx.sessionManager.getSessionFile && ctx.sessionManager.getSessionFile()) || "",
    cwd: (ctx && ctx.cwd) || "",
  })

  const record = (ctx, messages) => {
    const meta = sessionMeta(ctx)
    if (!meta.cwd) return
    let picked = fromMessages(messages)
    if ((!picked.prompt || !picked.last) && ctx && ctx.sessionManager && ctx.sessionManager.getBranch) {
      const branch = fromMessages(ctx.sessionManager.getBranch())
      if (!picked.prompt) picked.prompt = branch.prompt
      if (!picked.last) picked.last = branch.last
    }
    tell(end, {
      session_id: meta.id,
      cwd: meta.cwd,
      transcript_path: meta.file,
      prompt: picked.prompt,
      last_message: picked.last,
    })
  }

  pi.on("session_start", async (event, ctx) => {
    const meta = sessionMeta(ctx)
    const key = meta.id || meta.cwd
    if (!key || told.has(key)) return
    const reason = (event && event.reason) || "startup"
    const source = reason === "startup" || reason === "new" ? "startup" : reason
    told.set(key, ask(start, {
      session_id: meta.id,
      cwd: meta.cwd,
      transcript_path: meta.file,
      source,
    }))
  })

  pi.on("before_agent_start", async (_event, ctx) => {
    const meta = sessionMeta(ctx)
    const key = meta.id || meta.cwd
    if (!key || injected.has(key)) return
    injected.add(key)
    const msg = told.get(key)
    if (!msg) return
    return { message: { customType: "sous", content: msg, display: false } }
  })

  pi.on("agent_end", async (event, ctx) => {
    record(ctx, event && event.messages)
  })

  pi.on("session_before_compact", async (event, ctx) => {
    record(ctx, event && event.branchEntries)
  })

  pi.on("session_shutdown", async (_event, ctx) => {
    const meta = sessionMeta(ctx)
    record(ctx, null)
    const key = meta.id || meta.cwd
    if (key) {
      told.delete(key)
      injected.delete(key)
    }
  })
}

function ask(command, input) {
  try {
    const r = spawnSync("sh", ["-c", command], { input: JSON.stringify(input), encoding: "utf8", timeout })
    return r.status === 0 ? r.stdout.trim() : ""
  } catch {
    return ""
  }
}

function tell(command, input) {
  try {
    const child = spawn("sh", ["-c", command], { stdio: ["pipe", "ignore", "ignore"], detached: true })
    child.on("error", () => {})
    child.stdin.on("error", () => {})
    child.stdin.end(JSON.stringify(input))
    child.unref()
  } catch {}
}
