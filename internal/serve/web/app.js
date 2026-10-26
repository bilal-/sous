// sous board: draws the native integration protocol's JSON (version 1)
// by the display rules in integrations/design/README.md. Everything it
// shows comes from `sous integration watch`; everything it does is an
// action sous lists, sent with an op so a retry never does it twice.
"use strict";

const $ = (id) => document.getElementById(id);
const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
};

const state = { snap: null, items: new Map(), tokens: null, live: false, seen: 0 };

// ---- Tokens: one file for every surface ---------------------------------

async function loadTokens() {
  try {
    state.tokens = await (await fetch("/tokens.json")).json();
  } catch { return; }
  applyTheme();
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", applyTheme);
}

function applyTheme() {
  const t = state.tokens;
  if (!t) return;
  const mode = matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  const c = t.color[mode], s = document.documentElement.style;
  const set = (k, v) => s.setProperty(k, v);
  set("--bg", c.bg); set("--surface", c.surface); set("--surface2", c.surface2); set("--border", c.border);
  set("--text", c.text); set("--muted", c.textMuted); set("--faint", c.textFaint);
  set("--accent", c.accent); set("--on-accent", c.onAccent);
  const r = t.role;
  set("--needs", r.needs_you[mode]); set("--progress", r.in_progress[mode]); set("--ready", r.ready[mode]);
  set("--failed", r.failed[mode]); set("--on-you", r.on_you[mode]); set("--on-others", r.on_others[mode]);
  set("--unfinished", r.unfinished[mode]);
}

// ---- Display rules ------------------------------------------------------

const GROUPS = [
  { id: "needs_you", word: "needs you", role: "--needs", card: true },
  { id: "in_progress", word: "in progress", role: "--progress", card: true },
  { id: "ready", word: "ready to review", role: "--ready", card: true },
  { id: "on_you", word: "on you", role: "--on-you" },
  { id: "on_others", word: "on others", role: "--on-others" },
  { id: "unfinished", word: "unfinished", role: "--unfinished" },
];
const WORKING = ["queued", "planning", "working", "reviewing", "blocked"];
const STOPPED = ["failed", "stopped", "discarded"];

// groupOf is rule 2: runs by phase, everything else by section.
function groupOf(item) {
  const run = item.run;
  if (run && !item.closed_at) {
    const phase = run.phase || "";
    if (phase === "needs_you" || (!phase && run.state === "needs_you")) return "needs_you";
    if (["ready", "approved", "published"].includes(phase) || (!phase && run.state === "done")) return "ready";
    if (WORKING.includes(phase) || (!phase && (run.state === "running" || run.state === "starting"))) return "in_progress";
    if (phase && !STOPPED.includes(phase)) return "in_progress"; // unknown: as written, never as finished
  }
  return ["on_you", "on_others", "unfinished"].includes(item.section) ? item.section : null;
}

function age(iso) {
  if (!iso) return "";
  const s = Math.max(0, (Date.now() - Date.parse(iso)) / 1000);
  if (s < 60) return "now";
  if (s < 3600) return Math.floor(s / 60) + "m";
  if (s < 86400) return Math.floor(s / 3600) + "h";
  if (s < 86400 * 60) return Math.floor(s / 86400) + "d";
  return Math.floor(s / (86400 * 30)) + "mo";
}

const short = (sha) => (sha || "").slice(0, 12);
const plural = (n, w) => n + " " + w + (n === 1 ? "" : "s");

// ---- Applying the stream -----------------------------------------------

function applyEvent(e) {
  if (e.type === "unavailable") {
    setLive(false, e.error);
    return;
  }
  if (e.type === "snapshot" && e.snapshot) {
    state.snap = e.snapshot;
    state.items = new Map(e.snapshot.items.map((i) => [i.key, i]));
  } else if (e.type === "changes" && state.snap) {
    for (const c of e.changes || []) {
      if (c.type === "item.removed") state.items.delete(c.key);
      else if (c.item) state.items.set(c.key, c.item);
    }
    if (e.meta) Object.assign(state.snap, e.meta);
    if (e.snapshot) Object.assign(state.snap, e.snapshot); // version 0 style, if ever sent
  }
  state.seen = Date.now();
  setLive(true);
  render();
}

function connect() {
  const es = new EventSource("/api/events");
  es.onmessage = (m) => {
    try { applyEvent(JSON.parse(m.data)); } catch (err) { console.error(err); }
  };
  es.onerror = () => setLive(false);
  es.addEventListener("end", () => { es.close(); setTimeout(connect, 2000); });
}

function setLive(on, why) {
  state.live = on;
  const l = $("live");
  l.className = "live " + (on ? "on" : "off");
  if (on) l.textContent = "live";
  else l.textContent = state.seen ? "reconnecting · seen " + age(new Date(state.seen).toISOString()) + " ago" : (why || "connecting");
  if (on) resendPending();
}

// ---- Rendering ----------------------------------------------------------

function render() {
  const groups = Object.fromEntries(GROUPS.map((g) => [g.id, []]));
  for (const item of state.items.values()) {
    const g = groupOf(item);
    if (g) groups[g].push(item);
  }
  for (const g of GROUPS) groups[g.id].sort((a, b) => Date.parse(b.since) - Date.parse(a.since));
  groups.unfinished.reverse(); // oldest first, as on the terminal

  renderCounts(groups);
  renderProblems();
  const root = $("groups");
  root.replaceChildren();
  let any = false;
  for (const g of GROUPS) {
    const items = groups[g.id];
    if (!items.length) continue;
    any = true;
    const sec = el("section", "group");
    sec.style.setProperty("--role", `var(${g.role})`);
    const h = el("h2");
    h.append(g.word, " ", el("span", "n", String(items.length)));
    sec.append(h);
    if (g.card) for (const i of items) sec.append(card(g, i));
    else sec.append(rows(items, g));
    root.append(sec);
  }
  const omitted = state.snap && state.snap.omitted;
  $("empty").hidden = any || !!(state.snap && state.snap.problems && state.snap.problems.length);
  $("board").setAttribute("aria-busy", "false");
  if (omitted) {
    const n = Object.values(omitted).reduce((a, b) => a + b, 0);
    if (n) root.append(el("p", "faint", plural(n, "older item") + " not shown"));
  }
}

function renderCounts(groups) {
  const p = $("counts");
  p.replaceChildren();
  const gap = state.snap && state.snap.problems && state.snap.problems.length;
  const parts = [];
  for (const g of GROUPS) {
    const n = groups[g.id].length;
    if (n || gap) parts.push([n, g.word]);
  }
  if (gap) { const q = el("span", "gap", "? "); p.append(q); }
  if (!parts.length) { p.textContent = "nothing waiting"; return; }
  parts.forEach(([n, w], i) => {
    if (i) p.append(" · ");
    p.append(el("b", null, String(n)), " " + w);
  });
}

function renderProblems() {
  const box = $("problems");
  const probs = (state.snap && state.snap.problems) || [];
  box.hidden = !probs.length;
  box.textContent = probs.length ? "Not everything could be checked: " + probs.join(" · ") : "";
}

function head(item) {
  const h = el("div", "head");
  h.append(el("p", "title", item.text));
  const w = el("span", "where");
  w.append(el("span", "id", item.id), item.name + " · " + age(item.since));
  h.append(w);
  return h;
}

function card(g, item) {
  const c = el("article", "card");
  c.style.setProperty("--role", `var(${g.role})`);
  c.append(head(item));
  const run = item.run || {};
  const acts = new Set(item.actions || []);
  if (g.id === "needs_you") c.append(...questionParts(item, run, acts));
  if (g.id === "in_progress") c.append(stepPart(run));
  if (g.id === "ready") c.append(...resultParts(item, run, acts));
  if (run.error) c.append(el("p", "blocked", "Status unavailable: " + run.error));
  const a = el("div", "actions");
  if (acts.has("stop")) a.append(button("Stop", "quiet danger", () => confirmThen("Stop this run?", item.text, () => act("stop", item.id))));
  if (a.childElementCount) c.append(a);
  return c;
}

function questionParts(item, run, acts) {
  const q = run.question || { text: run.text };
  const out = [el("p", "question", q.text || "It is waiting on you.")];
  if (!acts.has("reply") || !q.id) return out;
  const send = (answer) => act("reply", item.id, { question: q.id + ":" + q.token, input: answer });
  if (q.choices && q.choices.length) {
    const cs = el("div", "choices");
    for (const ch of q.choices) cs.append(button(ch, "", () => send(ch)));
    out.push(cs);
  }
  if (q.other || !q.choices || !q.choices.length) {
    const o = el("div", "other");
    const t = el("textarea");
    t.placeholder = q.choices && q.choices.length ? "Or say it in your own words" : "Your answer";
    t.maxLength = 4000;
    t.setAttribute("aria-label", "Answer");
    o.append(t, button("Send", "primary", () => { if (t.value.trim()) send(t.value.trim()); }));
    out.push(o);
  }
  return out;
}

function stepPart(run) {
  const s = el("div", "step");
  const phase = run.phase || run.state || "running";
  let word = (run.progress && run.progress.step) || phase;
  if (phase === "blocked") { word = "blocked" + (run.reason ? ": " + run.reason : ""); s.classList.add("blocked"); }
  s.append(el("span", null, word));
  const p = run.progress;
  if (p && p.total > 0) {
    const bar = el("span", "bar");
    const fill = el("i");
    fill.style.width = Math.round((100 * p.done) / p.total) + "%";
    bar.append(fill);
    s.append(bar, el("span", "faint", p.done + " of " + p.total));
  }
  return s;
}

function resultParts(item, run, acts) {
  const out = [];
  const res = run.result;
  const pub = run.publication;
  if (res) {
    const r = el("div", "result");
    if (res.summary || run.text) r.append(el("p", "muted", res.summary || run.text));
    const facts = el("div", "facts");
    if (res.files) {
      facts.append(el("span", null, plural(res.files.changed, "file")));
      const ch = el("span");
      ch.append(el("span", "add", "+" + res.files.lines_added), " ", el("span", "del", "−" + res.files.lines_removed));
      facts.append(ch);
    }
    facts.append(el("span", "mono", short(res.commit)));
    if (res.review) facts.append(el("span", null, "reviewed by " + res.review.by + (res.review.independent ? "" : " (same vendor)")));
    r.append(facts);
    if (res.checks && res.checks.length) {
      const ul = el("ul", "checks");
      for (const ck of res.checks) {
        ul.append(el("li", ck.status, ck.name + " · " + ck.status.replace("_", " ") + (ck.required ? ", required" : "")));
      }
      r.append(ul);
    }
    if (res.checks || res.review) r.append(el("span", "reported", "checks and review as the runner reports them"));
    out.push(r);
  } else if (run.text) {
    out.push(el("p", "muted", run.text));
  }
  if (pub) {
    const p = el("div", "pub");
    p.append("Approved · " + pub.stage + (pub.branch ? " · " + pub.branch : ""));
    if (pub.pr) {
      const a = el("a", null, " · draft PR");
      a.href = pub.pr; a.target = "_blank"; a.rel = "noopener noreferrer";
      p.append(a);
    }
    if (pub.error) p.append(el("div", "err", "Stopped: " + pub.error));
    out.push(p);
  }
  const a = el("div", "actions");
  if (acts.has("diff")) a.append(button("View changes", "", () => showDiff(item, 1)));
  if (acts.has("approve") && res) {
    const label = pub ? "Carry on publishing" : "Approve";
    a.append(button(label, "ok", () => confirmThen(
      "Publish this result?",
      `sous pushes commit ${short(res.commit)} to ${run.branch || "the run's branch"} and opens a draft pull request.`,
      () => act("approve", item.id, { commit: res.commit }))));
  }
  if (acts.has("discard")) a.append(button("Discard", "quiet danger", () => confirmThen("Discard this result?", "Its work stays until you clean it up.", () => act("discard", item.id))));
  if (a.childElementCount) out.push(a);
  return out;
}

const ROWS_SHOWN = 12;

function rows(items, g) {
  const box = el("div", "rows");
  for (const item of items.slice(0, ROWS_SHOWN)) {
    const r = el("div", "row");
    const text = el("div", "text");
    const t = el("span", "t", item.run && item.run.state === "failed" ? "run failed · " + item.text : item.text);
    if (item.stale) t.append(el("span", "tag", "stale"));
    text.append(t, el("span", "p", item.name));
    const actions = el("div", "row-actions");
    const acts = new Set(item.actions || []);
    if (acts.has("done")) actions.append(button("Done", "", () => act("done", item.id)));
    if (acts.has("snooze")) actions.append(button("Snooze", "quiet", () => act("snooze", item.id)));
    r.append(el("span", "id", item.id), text, actions, el("span", "age", age(item.since)));
    box.append(r);
  }
  if (items.length > ROWS_SHOWN) box.append(el("div", "more", (items.length - ROWS_SHOWN) + " more on the terminal: sous"));
  return box;
}

function button(label, cls, onClick) {
  const b = el("button", cls, label);
  b.type = "button";
  b.addEventListener("click", onClick);
  return b;
}

// ---- Actions: one op per thing asked for (rule 9) -----------------------

const PENDING = "sous.pending";

function newOp() {
  const b = new Uint8Array(15);
  crypto.getRandomValues(b);
  return "op-" + btoa(String.fromCharCode(...b)).replace(/\+/g, "-").replace(/\//g, "_");
}

function pending() {
  try { return JSON.parse(localStorage.getItem(PENDING) || "{}"); } catch { return {}; }
}
function savePending(p) { localStorage.setItem(PENDING, JSON.stringify(p)); }

async function act(action, target, extra = {}) {
  const body = { action, op: newOp(), target, ...extra };
  const p = pending();
  p[body.op] = body;
  savePending(p);
  await send(body);
}

async function send(body) {
  let res, out;
  try {
    res = await fetch("/api/act", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    out = await res.json();
  } catch {
    toast("Not sent yet; it goes when sous is back.", "--needs");
    return;
  }
  if (out.outcome === "in_progress") return; // the first is still being done; ask again later
  const p = pending();
  delete p[body.op];
  savePending(p);
  const words = {
    done: "Done", already_done: "Done", refused: "Refused", conflict: "That changed in the meantime",
    failed: "Failed", not_ready: "sous is not set up for that",
  };
  const role = out.outcome === "done" || out.outcome === "already_done" ? "--ready" : out.outcome === "conflict" ? "--needs" : "--failed";
  let msg = (words[out.outcome] || out.error || "Something went wrong") + " · " + body.action;
  if (out.reason) msg += ": " + out.reason.replace(/_/g, " ");
  if (out.reason === "not_allowed_project") msg += " (sous config -p <project> remote_runs true)";
  toast(msg, role);
}

let resending = false;
async function resendPending() {
  if (resending) return;
  resending = true;
  for (const body of Object.values(pending())) await send(body);
  resending = false;
}

// ---- Dialogs ------------------------------------------------------------

function confirmThen(title, body, yes) {
  const d = $("confirm-dialog");
  $("confirm-title").textContent = title;
  $("confirm-body").textContent = body;
  d.returnValue = "";
  d.showModal();
  d.addEventListener("close", () => { if (d.returnValue === "yes") yes(); }, { once: true });
}

function openTask() {
  const d = $("task-dialog");
  const sel = $("task-project");
  sel.replaceChildren();
  const paths = (state.snap && state.snap.run_projects) || [];
  for (const path of paths) {
    const parts = path.split("/");
    const o = el("option", null, parts.slice(-2).join("/"));
    o.value = path;
    sel.append(o);
  }
  $("task-none").hidden = paths.length > 0;
  sel.disabled = $("task-brief").disabled = $("task-start").disabled = paths.length === 0;
  d.showModal();
  $("task-brief").focus();
}

async function showDiff(item, page) {
  const d = $("diff-dialog");
  const body = $("diff-body");
  body.textContent = "Loading…";
  if (!d.open) d.showModal();
  let r;
  try {
    r = await (await fetch(`/api/diff?id=${encodeURIComponent(item.id)}&page=${page}`)).json();
  } catch { body.textContent = "Could not read the changes."; return; }
  if (r.error) { body.textContent = r.error; return; }
  $("diff-title").textContent = item.text;
  $("diff-commit").textContent = short(r.base) + " → " + short(r.commit);
  body.replaceChildren();
  for (const line of r.patch.split("\n")) {
    const cls = line.startsWith("diff --git") ? "f" : line.startsWith("@@") ? "h" : line.startsWith("+") && !line.startsWith("+++") ? "a" : line.startsWith("-") && !line.startsWith("---") ? "d" : null;
    body.append(el("span", cls, line + "\n"));
  }
  $("diff-page").textContent = r.pages > 1 ? `page ${r.page} of ${r.pages}` : "";
  $("diff-prev").disabled = r.page <= 1;
  $("diff-next").disabled = r.page >= r.pages;
  $("diff-prev").onclick = () => showDiff(item, r.page - 1);
  $("diff-next").onclick = () => showDiff(item, r.page + 1);
}

function toast(text, role) {
  const t = el("div", "toast", text);
  if (role) t.style.setProperty("--role", `var(${role})`);
  $("toasts").append(t);
  setTimeout(() => t.remove(), 6000);
}

// ---- Start --------------------------------------------------------------

document.addEventListener("DOMContentLoaded", () => {
  for (const b of document.querySelectorAll("[data-close]")) b.addEventListener("click", () => b.closest("dialog").close());
  $("confirm-yes").value = "yes";
  $("new-task").addEventListener("click", openTask);
  $("task-form").addEventListener("submit", (e) => {
    const brief = $("task-brief").value.trim();
    const project = $("task-project").value;
    if (!brief || !project) { e.preventDefault(); return; }
    act("run", project, { input: brief });
    $("task-brief").value = "";
  });
  setInterval(() => { if (state.snap) render(); if (!state.live) setLive(false); }, 30000);
  loadTokens();
  connect();
});
