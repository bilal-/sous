// sous board: draws the native integration protocol's JSON (version 1)
// by the display rules in integrations/design/README.md. Everything it
// shows comes from `sous integration watch`; everything it does is an
// action sous lists, sent with an op so a retry never does it twice.
//
// The page is three panes: views and projects on the left, the list in
// the middle (searched, filtered, grouped), and one item in full on the
// right. Where you are lives in the address, so back and forward work.
"use strict";

const $ = (id) => document.getElementById(id);
const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
};

const state = {
  snap: null, items: new Map(), tokens: null, live: false, seen: 0,
  // Where the person is: kept in the address.
  view: "inbox", project: null, q: "", open: null,
  // How they like the list: kept in this browser.
  group: {}, sort: "new", sources: new Set(), shut: new Set(),
  cur: null, selected: new Set(), leaving: new Set(), drafts: {},
  order: [], shownDetail: "", pressed: 0, held: false, pushedOpen: false,
};

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
  const set = (k, v) => v && s.setProperty(k, v);
  set("--bg", c.bg); set("--surface", c.surface); set("--surface2", c.surface2); set("--border", c.border);
  set("--text", c.text); set("--muted", c.textMuted); set("--faint", c.textFaint);
  set("--accent", c.accent); set("--on-accent", c.onAccent);
  const r = t.role, role = (k) => r[k] && r[k][mode];
  set("--needs", role("needs_you")); set("--progress", role("in_progress")); set("--ready", role("ready"));
  set("--failed", role("failed")); set("--on-you", role("on_you")); set("--on-others", role("on_others"));
  set("--unfinished", role("unfinished")); set("--idea", role("idea"));
}

// ---- Display rules ------------------------------------------------------

// Every status an item can be in, in the order the person acts on them.
const STATUS = [
  { id: "needs_you", word: "needs you", role: "--needs" },
  { id: "in_progress", word: "in progress", role: "--progress" },
  { id: "ready", word: "ready to review", role: "--ready" },
  { id: "on_you", word: "on you", role: "--on-you" },
  { id: "on_others", word: "on others", role: "--on-others" },
  { id: "ideas", word: "idea", role: "--idea" },
  { id: "unfinished", word: "unfinished", role: "--unfinished" },
];
const STATUS_OF = Object.fromEntries(STATUS.map((s) => [s.id, s]));
const RUN_GROUPS = ["needs_you", "in_progress", "ready"];
const WORKING = ["queued", "planning", "working", "reviewing", "blocked"];
const STOPPED = ["failed", "stopped", "discarded"];

// The views on the left. The inbox is the board: what to act on, not ideas
// or unfinished branches (rule 1); those have views of their own.
const VIEWS = [
  { id: "inbox", word: "Inbox", has: (s) => RUN_GROUPS.includes(s) || s === "on_you", group: "status",
    empty: ["Nothing needs you", "No runs waiting and nothing on you."] },
  { id: "runs", word: "Agent runs", role: "--progress", has: (s, i) => !!i.run, group: "status",
    empty: ["No agent runs", "Start one with New task, or hand a note to an agent from its page."] },
  { sep: true },
  { id: "on_you", word: "On you", role: "--on-you", has: (s) => s === "on_you", group: "project",
    empty: ["Nothing on you", "Notes you keep for yourself show here."] },
  { id: "on_others", word: "Waiting on others", role: "--on-others", has: (s) => s === "on_others", group: "project",
    empty: ["Nothing waiting on others", "Reviews and replies you are waiting for show here."] },
  { id: "ideas", word: "Ideas", role: "--idea", has: (s) => s === "ideas", group: "project",
    empty: ["No ideas", "Ideas you note for later show here."] },
  { id: "unfinished", word: "Unfinished work", role: "--unfinished", has: (s) => s === "unfinished", group: "project",
    empty: ["Nothing unfinished", "Uncommitted changes and unpushed branches show here."] },
  { sep: true },
  { id: "all", word: "Everything", has: () => true, group: "status", empty: ["Nothing at all", "Your board is empty."] },
];
const VIEW_OF = Object.fromEntries(VIEWS.filter((v) => v.id).map((v) => [v.id, v]));

// Where an item came from, for the filter chips.
const SOURCES = [
  { id: "note", word: "Your notes", has: (i) => i.source === "human" },
  { id: "agent", word: "Agent notes", has: (i) => i.source === "agent" },
  { id: "git", word: "Git", has: (i) => i.source === "git" },
  { id: "tracker", word: "Trackers", has: (i) => !["human", "agent", "git"].includes(i.source) },
];
const sourceOf = (i) => (SOURCES.find((s) => s.has(i)) || SOURCES[3]).id;

// statusOf is rule 2: runs by phase, everything else by section.
function statusOf(item) {
  const run = item.run;
  if (run && !item.closed_at) {
    const phase = run.phase || "";
    if (phase === "needs_you" || (!phase && run.state === "needs_you")) return "needs_you";
    if (["ready", "approved", "published"].includes(phase) || (!phase && run.state === "done")) return "ready";
    if (WORKING.includes(phase) || (!phase && (run.state === "running" || run.state === "starting"))) return "in_progress";
    if (phase && !STOPPED.includes(phase)) return "in_progress"; // unknown: as written, never as finished
  }
  return STATUS_OF[item.section] ? item.section : null;
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
const when = (iso) => iso ? new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }) : "";
const short = (sha) => (sha || "").slice(0, 12);
const plural = (n, w) => n + " " + w + (n === 1 ? "" : "s");

function projectOf(path) {
  const p = ((state.snap && state.snap.projects) || []).find((x) => x.path === path);
  const parts = (path || "").split("/");
  return p || { path, name: parts[parts.length - 1] || path, org: parts[parts.length - 2] || "" };
}
// The project's page on its forge, from origin's host/org/repo. An ssh
// alias (a host with no dot) has no page to open.
function siteOf(p) {
  const host = p && p.remote ? p.remote.split("/")[0] : "";
  if (!host.includes(".") || !p.remote.includes("/")) return null;
  const word = { "github.com": "GitHub", "gitlab.com": "GitLab", "bitbucket.org": "Bitbucket", "codeberg.org": "Codeberg" }[host] || host;
  return { href: "https://" + p.remote, word };
}
function siteLink(p, cls, text) {
  const site = siteOf(p);
  if (!site) return null;
  const a = el("a", cls, text === undefined ? site.word + " ↗" : text);
  a.href = site.href; a.target = "_blank"; a.rel = "noopener noreferrer";
  a.title = "Open " + p.remote + " in a new tab";
  a.addEventListener("click", (e) => e.stopPropagation());
  return a;
}
const canRun = (path) => ((state.snap && state.snap.run_projects) || []).includes(path);

// A tracker ref as words and, when it can be, a link.
function refOf(ref) {
  if (!ref) return null;
  let m = ref.match(/^github:([^#]+)#(\d+)$/);
  if (m) return { word: m[1] + "#" + m[2], href: `https://github.com/${m[1]}/issues/${m[2]}` };
  m = ref.match(/^gitlab:([^!#]+)([!#])(\d+)$/);
  if (m) return { word: m[1] + m[2] + m[3], href: `https://${m[1]}/-/${m[2] === "!" ? "merge_requests" : "issues"}/${m[3]}` };
  m = ref.match(/^md:([^:]+):/);
  if (m) return { word: "filed in " + m[1] };
  return { word: ref };
}

// ---- Plain words --------------------------------------------------------
//
// Items carry the words sous and agents wrote: dense, written for a
// terminal row. present() turns one into what a person reads first: a short
// title, an optional kicker above it, the rest as a body, and for git rows
// what the finding means and what to do about it. It only rearranges and
// explains; the item's own text stays one tap away.

const cap = (s) => s ? s[0].toUpperCase() + s.slice(1) : s;
const count = (n, one, many) => n + " " + (+n === 1 ? one : many);

function present(item) {
  const text = (item.text || "").trim();
  if (item.source === "git") return gitWords(text);
  const m = text.match(/^([^·]{3,40}) · ((?:PR #|MR !|issue #)\d+) (.+)$/s);
  if (m) {
    const review = /review requested/.test(m[1]);
    return { kicker: cap(m[1].trim()) + " · " + m[2], title: m[3], body: "",
      means: review ? "Someone asked you to review this. Open it, review, and it leaves your list once you've responded."
        : /awaiting review/.test(m[1]) ? "You opened this and it's waiting for a reviewer. Nothing to do unless it has stalled."
        : /checks failing/.test(m[1]) ? "The latest checks on your pull request failed. Open it to see which."
        : /changes requested/.test(m[1]) ? "A reviewer asked for changes on your pull request."
        : "" };
  }
  let rest = text, kicker = "";
  const k = rest.match(/^([A-Za-z][\w ./+-]{1,32}):\s+(\S[\s\S]*)$/);
  if (k && k[1].trim().split(/\s+/).length <= 4 && !/^https?$/i.test(k[1])) { kicker = cap(k[1].trim()); rest = cap(k[2]); }
  const [title, body] = rest.length > 110 ? firstSentence(rest) : [rest, ""];
  return { kicker, title, body };
}

// firstSentence splits at the end of the first sentence that is long
// enough to stand alone, never inside "e.g." or a version number.
function firstSentence(s) {
  const re = /[.!?](?=\s+[A-Z0-9(`'"~\[])/g;
  let m;
  while ((m = re.exec(s))) {
    const head = s.slice(0, m.index + 1);
    if (head.length < 24) continue;
    if (/\b(e\.g|i\.e|etc|vs|cf|approx|No)\.$/i.test(head) || /\b[A-Za-z]\.$/.test(head)) continue;
    if (head.length > 180) break;
    return [head, s.slice(m.index + 1).trim()];
  }
  return [s, ""];
}

// gitWords explains what the git signal found, as the person would say it.
function gitWords(text) {
  let wt = "";
  const w = text.match(/^(.*) · worktree (.+)$/);
  if (w) { text = w[1]; wt = w[2]; }
  let m = text.match(/^(\d+) files? uncommitted · (.+)$/);
  if (m) return { title: count(m[1], "uncommitted file", "uncommitted files"), branch: m[2], worktree: wt, git: "dirty",
    means: "Changes in the working folder that aren't committed yet. Commit them, stash them, or throw them away." };
  m = text.match(/^(\d+) commits? unpushed · (.+)$/);
  if (m) return { title: count(m[1], "commit", "commits") + " not pushed", branch: m[2], worktree: wt, git: "ahead",
    means: "Committed on this computer but not on the remote yet. Until you push, they exist only here." };
  m = text.match(/^(.+) has no upstream$/);
  if (m) return { title: "Branch never pushed", branch: m[1], worktree: wt, git: "no-upstream",
    means: "This branch exists only on this computer. Push it to keep it, or delete it if it's finished with." };
  m = text.match(/^(\d+) stash(?:es)?$/);
  if (m) return { title: count(m[1], "stashed change", "stashed changes"), worktree: wt, git: "stash",
    means: "Work set aside with git stash. Look through it and drop what you no longer need." };
  return { title: text, worktree: wt, git: "other" };
}

// The commands that deal with a git finding, run in the project's folder.
function gitSteps(words, branch) {
  switch (words.git) {
    case "dirty": return [["git status", "See which files changed"], ["git diff", "Read the changes"]];
    case "ahead": return [["git log @{u}..", "See the commits not pushed"], ["git push", "Push them"]];
    case "no-upstream": return [["git push -u origin " + branch, "Push the branch and track it"], ["git branch -d " + branch, "Or delete it, once merged"]];
    case "stash": return [["git stash list", "See what's stashed"], ["git stash drop", "Drop the newest one"]];
  }
  return [];
}

const FROM = { human: "Your note", agent: "A note an agent left", git: "Git, in your working copy", github: "GitHub", gitlab: "GitLab" };

// ago says how long since, in whole words: "4 days ago", "just now".
function ago(iso) {
  const s = Math.max(0, (Date.now() - Date.parse(iso)) / 1000);
  const units = [[86400 * 365, "year"], [86400 * 30, "month"], [86400 * 7, "week"], [86400, "day"], [3600, "hour"], [60, "minute"]];
  for (const [n, w] of units) if (s >= n) return plural(Math.floor(s / n), w) + " ago";
  return "just now";
}

// linked appends text to node with web addresses as links.
function linked(node, text) {
  const re = /https?:\/\/[^\s)<>"']+[^\s)<>"'.,;:!?]/g;
  let at = 0, m;
  while ((m = re.exec(text))) {
    if (m.index > at) node.append(text.slice(at, m.index));
    const a = el("a", null, m[0]);
    a.href = m[0]; a.target = "_blank"; a.rel = "noopener noreferrer";
    node.append(a);
    at = m.index + m[0].length;
  }
  if (at < text.length) node.append(text.slice(at));
  return node;
}

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
  for (const k of state.leaving) if (!state.items.has(k)) state.leaving.delete(k);
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

// ---- Where the person is ------------------------------------------------

function readAddress() {
  const h = new URLSearchParams(location.hash.slice(1));
  state.project = h.get("project");
  state.view = state.project ? "project" : (VIEW_OF[h.get("view")] ? h.get("view") : "inbox");
  state.q = h.get("q") || "";
  state.open = h.get("item");
  if ($("search").value.trim() !== state.q) $("search").value = state.q;
}

function go(change, replace) {
  const next = { view: state.view, project: state.project, q: state.q, open: state.open, ...change };
  const h = new URLSearchParams();
  if (next.project) h.set("project", next.project);
  else if (next.view && next.view !== "inbox") h.set("view", next.view);
  if (next.q) h.set("q", next.q);
  if (next.open) h.set("item", next.open);
  const hash = "#" + h.toString();
  if (hash === location.hash || (hash === "#" && !location.hash)) return;
  if (replace) history.replaceState(null, "", hash === "#" ? location.pathname : hash);
  else history.pushState(null, "", hash === "#" ? location.pathname : hash);
  readAddress();
  render();
}

function loadPrefs() {
  try {
    const p = JSON.parse(localStorage.getItem("sous.view") || "{}");
    state.group = p.group || {};
    state.sort = p.sort || "new";
    state.sources = new Set(p.sources || []);
    state.shut = new Set(p.shut || []);
  } catch { /* defaults */ }
}
function savePrefs() {
  localStorage.setItem("sous.view", JSON.stringify({
    group: state.group, sort: state.sort, sources: [...state.sources], shut: [...state.shut],
  }));
}
const viewKey = () => state.project ? "project" : state.view;
function groupBy() {
  const v = state.project ? { group: "status" } : VIEW_OF[state.view];
  return state.group[viewKey()] || v.group;
}

// ---- Choosing what to show ----------------------------------------------

function inView(item) {
  const s = statusOf(item);
  if (!s) return false;
  if (state.project) return item.project === state.project;
  return VIEW_OF[state.view].has(s, item);
}

function matches(item) {
  if (!state.q) return true;
  const hay = [item.text, item.name, item.project, item.id, item.ref, item.run && item.run.text,
    item.run && item.run.question && item.run.question.text, STATUS_OF[statusOf(item)].word].join(" ").toLowerCase();
  return state.q.toLowerCase().split(/\s+/).filter(Boolean).every((w) => hay.includes(w));
}

function visible() {
  const base = [...state.items.values()].filter((i) => inView(i) && matches(i));
  const shown = base.filter((i) => !state.sources.size || state.sources.has(sourceOf(i)));
  const dir = state.sort === "old" ? 1 : -1;
  shown.sort((a, b) => dir * (Date.parse(a.since) - Date.parse(b.since)));
  return { base, shown };
}

function grouped(items) {
  const by = groupBy();
  if (by === "none") return [{ id: "all", items }];
  if (by === "status") {
    return STATUS.map((s) => ({ id: s.id, status: s, items: items.filter((i) => statusOf(i) === s.id) }))
      .filter((g) => g.items.length);
  }
  const m = new Map();
  for (const i of items) {
    if (!m.has(i.project)) m.set(i.project, []);
    m.get(i.project).push(i);
  }
  return [...m.entries()]
    .map(([path, items]) => ({ id: path, project: projectOf(path), items }))
    .sort((a, b) => b.items.length - a.items.length || a.project.name.localeCompare(b.project.name));
}

// ---- Rendering ----------------------------------------------------------

// showSide slides the views and projects in over the list on a narrow
// screen; elsewhere the side is always there and this does nothing visible.
function showSide(on) {
  $("side").classList.toggle("shown", on);
  $("menu").setAttribute("aria-expanded", String(on));
  syncScrim();
}

// The scrim dims what is under a drawer: the side, or the detail where it
// slides over the list. Tapping it closes the drawer.
function syncScrim() {
  const side = $("side").classList.contains("shown");
  const overlay = !!state.open && typeof matchMedia === "function" && matchMedia("(min-width: 761px) and (max-width: 1180px)").matches;
  $("scrim").hidden = !(side || overlay);
}

function render() {
  if (!state.snap) return;
  renderCounts();
  renderProblems();
  renderSide();
  renderList();
  renderDetail();
  syncScrim();
  $("board").setAttribute("aria-busy", "false");
}

function statusCounts(items) {
  const n = Object.fromEntries(STATUS.map((s) => [s.id, 0]));
  for (const i of items) {
    const s = statusOf(i);
    if (s) n[s]++;
  }
  return n;
}

function renderCounts() {
  const p = $("counts");
  p.replaceChildren();
  const n = statusCounts(state.items.values());
  const gap = state.snap.problems && state.snap.problems.length;
  const parts = STATUS.filter((s) => s.id !== "ideas" && (n[s.id] || gap)).map((s) => [n[s.id], s.word]);
  if (gap) p.append(el("span", "gap", "? "));
  if (!parts.length) { p.textContent = "nothing waiting"; return; }
  parts.forEach(([c, w], i) => {
    if (i) p.append(" · ");
    p.append(el("b", null, String(c)), " " + w);
  });
}

function renderProblems() {
  const box = $("problems");
  const probs = state.snap.problems || [];
  box.hidden = !probs.length;
  box.textContent = probs.length ? "Not everything could be checked, so counts may be short: " + probs.join(" · ") : "";
}

function navButton(word, count, on, onClick, role, hot) {
  const b = el("button", "nav" + (on ? " on" : "") + (hot ? " hot" : ""));
  b.type = "button";
  if (role) b.style.setProperty("--role", `var(${role})`);
  b.append(el("span", "dot"), el("span", "label", word));
  if (count) b.append(el("span", "n", String(count)));
  if (on) b.setAttribute("aria-current", "page");
  b.addEventListener("click", () => { onClick(); showSide(false); });
  return b;
}

function renderSide() {
  const all = [...state.items.values()];
  const views = $("views");
  views.replaceChildren();
  const needs = all.filter((i) => statusOf(i) === "needs_you").length;
  for (const v of VIEWS) {
    if (v.sep) { views.append(el("li", "sep")); continue; }
    const n = all.filter((i) => { const s = statusOf(i); return s && v.has(s, i); }).length;
    const li = el("li");
    li.append(navButton(v.word, n, !state.project && state.view === v.id, () => go({ view: v.id, project: null, open: null }),
      v.role || (v.id === "inbox" && needs ? "--needs" : null), v.id === "inbox" && needs > 0));
    views.append(li);
  }

  const box = $("projects");
  box.replaceChildren();
  const filter = $("project-filter").value.trim().toLowerCase();
  const counts = new Map();
  for (const i of all) if (statusOf(i)) counts.set(i.project, (counts.get(i.project) || 0) + 1);
  const projects = [...counts.keys()].map(projectOf)
    .filter((p) => !filter || (p.org + "/" + p.name).toLowerCase().includes(filter));
  const orgs = new Map();
  for (const p of projects) {
    if (!orgs.has(p.org)) orgs.set(p.org, []);
    orgs.get(p.org).push(p);
  }
  const orgTotal = (ps) => ps.reduce((a, p) => a + counts.get(p.path), 0);
  for (const [org, ps] of [...orgs.entries()].sort((a, b) => orgTotal(b[1]) - orgTotal(a[1]))) {
    box.append(el("div", "org", org || "projects"));
    const ul = el("ul", "plist");
    ps.sort((a, b) => counts.get(b.path) - counts.get(a.path) || a.name.localeCompare(b.name));
    for (const p of ps) {
      const li = el("li");
      const hot = all.some((i) => i.project === p.path && statusOf(i) === "needs_you");
      li.append(navButton(p.name, counts.get(p.path), state.project === p.path,
        () => go({ project: p.path, view: null, open: null }), hot ? "--needs" : null, hot));
      const site = siteLink(p, "ext", "↗");
      if (site) { site.setAttribute("aria-label", site.title); li.append(site); }
      ul.append(li);
    }
    box.append(ul);
  }
  const quiet = ((state.snap.projects) || []).length - counts.size;
  if (quiet > 0 && !filter) box.append(el("p", "quiet-projects", plural(quiet, "other project") + " with nothing waiting"));
  if (filter && !projects.length) box.append(el("p", "quiet-projects", "No project matches"));
}

function renderList() {
  const { base, shown } = visible();
  const v = state.project ? null : VIEW_OF[state.view];
  const proj = state.project && projectOf(state.project);
  $("view-title").textContent = proj ? proj.name : v.word;
  $("view-count").textContent = shown.length === base.length ? String(shown.length) : `${shown.length} of ${base.length}`;
  const runHere = $("view-run");
  runHere.hidden = !(proj && canRun(proj.path));
  const site = $("view-site");
  site.replaceChildren();
  const link = proj && siteLink(proj, "site");
  if (link) site.append(link);
  document.title = (proj ? proj.name : v.word) + " · sous";

  // Source chips: only the sources this view has, with how many each.
  const chips = $("sources");
  chips.replaceChildren();
  const present = SOURCES.filter((s) => base.some((i) => sourceOf(i) === s.id));
  if (present.length > 1) {
    for (const s of present) {
      const on = state.sources.has(s.id);
      const b = el("button", "chip" + (on ? " on" : ""), s.word);
      b.type = "button";
      b.setAttribute("aria-pressed", String(on));
      b.append(el("span", "n", String(base.filter((i) => sourceOf(i) === s.id).length)));
      b.addEventListener("click", () => {
        if (on) state.sources.delete(s.id); else state.sources.add(s.id);
        savePrefs(); render();
      });
      chips.append(b);
    }
  }
  $("group-by").value = groupBy();
  $("sort-by").value = state.sort;

  const root = $("groups");
  root.replaceChildren();
  const groups = grouped(shown);
  state.order = [];
  for (const g of groups) root.append(groupBlock(g));
  if (state.cur && !state.order.includes(state.cur)) state.cur = null;

  const empty = $("empty");
  empty.hidden = shown.length > 0;
  empty.replaceChildren();
  if (!shown.length) {
    const [title, body] = state.q ? ["Nothing matches “" + state.q + "”", "Try fewer words, or search Everything."]
      : base.length ? ["All filtered out", "Turn a source back on above."]
      : proj ? ["Nothing waiting in " + proj.name, "It is all done here."] : v.empty;
    empty.append(el("h2", null, title), el("p", null, body));
    if (state.q && state.view !== "all") empty.append(button("Search everything", "", () => go({ view: "all", project: null })));
  }
  renderBulk();
}

function groupBlock(g) {
  const sec = el("section", "group");
  const key = groupBy() + ":" + g.id;
  if (state.shut.has(key)) sec.classList.add("shut");
  if (g.status) sec.style.setProperty("--role", `var(${g.status.role})`);
  if (g.id !== "all") {
    const h = el("h2");
    const t = el("button");
    t.type = "button";
    t.setAttribute("aria-expanded", String(!state.shut.has(key)));
    t.append(el("span", "caret", "▾"));
    if (g.status) t.append(el("span", "dot"), g.status.word);
    else t.append(g.project.name, el("span", "org", g.project.org));
    t.append(el("span", "n", String(g.items.length)));
    t.addEventListener("click", () => {
      if (state.shut.has(key)) state.shut.delete(key); else state.shut.add(key);
      savePrefs(); renderList();
    });
    h.append(t);
    if (g.project) {
      const open = el("a", "go", "Open project");
      open.href = "#project=" + encodeURIComponent(g.project.path);
      const site = siteLink(g.project, "go");
      h.append(open);
      if (site) h.append(site);
    }
    sec.append(h);
  }
  const box = el("div", "rows");
  for (const item of g.items) {
    box.append(row(item, g));
    if (!state.shut.has(key)) state.order.push(item.key);
  }
  sec.append(box);
  return sec;
}

function row(item, g) {
  const s = STATUS_OF[statusOf(item)];
  const r = el("div", "row");
  r.dataset.key = item.key;
  r.setAttribute("role", "button");
  r.tabIndex = -1;
  if (item.key === state.cur || item.key === state.open) r.classList.add("cur");
  if (state.leaving.has(item.key)) r.classList.add("leaving");

  const pick = el("input");
  pick.type = "checkbox";
  pick.checked = state.selected.has(item.key);
  pick.setAttribute("aria-label", "Select");
  pick.addEventListener("click", (e) => { e.stopPropagation(); toggleSelect(item.key); });

  const body = el("div", "body");
  const run = item.run;
  const failed = run && (run.phase === "failed" || run.state === "failed");
  const words = present(item);
  if (words.kicker) body.append(el("div", "kicker", words.kicker));
  body.append(el("div", "t", (failed ? "Run failed · " : "") + words.title));
  if (s.id === "needs_you") {
    const q = (run.question && run.question.text) || run.text;
    if (q) body.append(el("div", "ask", "Asks: " + q));
  }
  const meta = el("div", "meta");
  if (words.branch) meta.append(el("span", "branch", words.branch));
  if (words.worktree) meta.append(el("span", null, "worktree " + words.worktree));
  if (!g.status) {
    const b = el("span", "badge", s.word);
    b.style.setProperty("--role", `var(${s.role})`);
    meta.append(b);
  }
  if (!state.project && !g.project) {
    const p = el("a", "proj", item.name);
    p.href = "#project=" + encodeURIComponent(item.project);
    p.addEventListener("click", (e) => e.stopPropagation());
    meta.append(p);
  }
  if (run && s.id === "in_progress") meta.append(...progressBits(run));
  if (run && s.id === "ready" && run.result && run.result.files) meta.append(el("span", null, plural(run.result.files.changed, "file") + " changed"));
  const ref = refOf(item.ref);
  if (ref && !words.kicker) meta.append(el("span", "tag", ref.word));
  if (item.source === "agent") meta.append(el("span", "tag", "from an agent"));
  if (item.stale) meta.append(el("span", "tag warn", "stale"));
  if (meta.childElementCount) body.append(meta);

  const right = el("div", "side-r");
  const acts = el("div", "acts");
  const has = new Set(item.actions || []);
  if (has.has("done")) acts.append(button("Done", "small", (e) => { e.stopPropagation(); quick("done", [item]); }));
  if (has.has("snooze")) acts.append(button("Snooze", "small quiet", (e) => { e.stopPropagation(); quick("snooze", [item]); }));
  right.append(acts, el("span", "age", age(item.since)));
  right.title = when(item.since);

  r.append(pick, body, right);
  // Touch: a long press selects; while selecting, a tap adds or removes.
  // A mouse selects with the checkbox, or x.
  // The list redraws when a press selects, so the click that follows may
  // land on the new row: state.pressed, not this row, remembers the press.
  let press = null;
  const cancel = () => { if (press) clearTimeout(press.t); press = null; };
  r.addEventListener("pointerdown", (e) => {
    if (e.pointerType !== "touch") return;
    press = { x: e.clientX, y: e.clientY, t: setTimeout(() => {
      press = null; state.held = true;
      if (navigator.vibrate) navigator.vibrate(10);
      toggleSelect(item.key);
    }, 450) };
  });
  r.addEventListener("pointermove", (e) => { if (press && Math.hypot(e.clientX - press.x, e.clientY - press.y) > 10) cancel(); });
  r.addEventListener("pointerup", cancel);
  r.addEventListener("pointercancel", cancel);
  r.addEventListener("contextmenu", (e) => { if (press || state.held) e.preventDefault(); });
  r.addEventListener("click", () => {
    if (state.held || Date.now() - state.pressed < 350) return;
    if (state.selected.size && coarse()) { toggleSelect(item.key); return; }
    openItem(item.key);
  });
  return r;
}

const coarse = () => typeof matchMedia === "function" && matchMedia("(pointer: coarse)").matches;

function progressBits(run) {
  const out = [];
  const phase = run.phase || run.state || "running";
  let word = (run.progress && run.progress.step) || phase;
  if (phase === "blocked") word = "blocked" + (run.reason ? ": " + run.reason : "");
  out.push(el("span", phase === "blocked" ? "blocked" : null, word));
  const p = run.progress;
  if (p && p.total > 0) {
    const bar = el("span", "mini-bar");
    const fill = el("i");
    fill.style.width = Math.round((100 * p.done) / p.total) + "%";
    bar.append(fill);
    out.push(bar, el("span", null, p.done + " of " + p.total));
  }
  return out;
}

// ---- The detail pane ----------------------------------------------------

function openItem(key) {
  state.cur = key;
  const was = state.open;
  go({ open: key }, !!was); // moving between items replaces, so Back returns to the list
  if (!was) state.pushedOpen = true;
}

// closeItem goes back to the list: back in history when opening the item
// was the last step, so the phone's back gesture and this button agree.
function closeItem() {
  if (state.pushedOpen && history.length > 1) { state.pushedOpen = false; history.back(); return; }
  go({ open: null });
}

function renderDetail() {
  const pane = $("detail");
  const item = state.open && state.items.get(state.open);
  document.querySelector(".app").classList.toggle("open", !!item);
  pane.hidden = !item;
  if (!item) { pane.replaceChildren(); state.shownDetail = ""; return; }
  // Redraw only when the item changed, so a half-typed answer survives updates.
  const sig = JSON.stringify(item) + (state.snap.run_projects || []).join();
  if (sig === state.shownDetail) return;
  state.shownDetail = sig;
  pane.replaceChildren(...detailParts(item));
}

function detailParts(item) {
  const s = STATUS_OF[statusOf(item)] || (item.section === "snoozed"
    ? { id: "snoozed", word: "snoozed", role: "--muted" } : null);
  const run = item.run;
  const has = new Set(item.actions || []);
  const p = projectOf(item.project);
  const words = present(item);
  const isNote = /^\d+$/.test(item.id);
  const out = [];

  // A bar to leave by, and to step through the list without leaving.
  const head = el("div", "d-head");
  const back = button("Back", "quiet back", closeItem);
  back.setAttribute("aria-label", "Back to the list");
  const b = el("span", "badge", s.word);
  b.style.setProperty("--role", `var(${s.role})`);
  head.append(back, b, el("span", "id", isNote ? "#" + item.id : ""));
  const step = el("div", "step-nav");
  const prev = button("↑", "quiet icon", () => moveCur(-1));
  prev.setAttribute("aria-label", "Previous item");
  prev.title = "Previous (k)";
  const next = button("↓", "quiet icon", () => moveCur(1));
  next.setAttribute("aria-label", "Next item");
  next.title = "Next (j)";
  const close = button("✕", "quiet icon close", closeItem);
  close.setAttribute("aria-label", "Close");
  close.title = "Close (Esc)";
  step.append(prev, next, close);
  head.append(step);
  out.push(head);

  const top = el("div", "d-top");
  if (words.kicker) top.append(el("p", "d-kicker", words.kicker));
  top.append(linked(el("h2", "d-text"), words.title));
  if (words.branch || words.worktree) {
    const where = el("p", "d-where");
    if (words.branch) where.append("on ", el("span", "branch", words.branch));
    if (words.worktree) where.append((words.branch ? " in worktree " : "in worktree "), el("b", null, words.worktree));
    top.append(where);
  }
  for (const para of (words.body || "").split(/\n{2,}/).filter(Boolean)) top.append(linked(el("p", "d-body"), para));
  out.push(top);

  // What to do, biggest first: the forge page for a tracker item, then
  // done and snooze, then handing it to an agent.
  const acts = el("div", "d-actions");
  const ref = refOf(item.ref);
  if (ref && ref.href && !isNote) {
    const a = el("a", "btn primary", "Open on " + (item.source === "gitlab" ? "GitLab" : "GitHub") + " ↗");
    a.href = ref.href; a.target = "_blank"; a.rel = "noopener noreferrer";
    acts.append(a);
  }
  if (has.has("done")) acts.append(button("Done", "ok", () => quick("done", [item])));
  if (has.has("snooze")) acts.append(button("Snooze", "", () => quick("snooze", [item])));
  const agentable = !run && isNote && item.section !== "unfinished";
  if (agentable && canRun(item.project)) acts.append(button("Hand to an agent", "primary", () => openTask(item.project, item.text)));
  if (acts.childElementCount) out.push(acts);

  if (run) out.push(runSection(item, run, s, has));

  if (words.means) {
    const sec = el("div", "d-section");
    sec.append(el("h3", null, "What this means"), el("p", "d-means", words.means));
    const steps = gitSteps(words, words.branch);
    if (steps.length) {
      sec.append(el("p", "hint", "In " + p.name + (words.worktree ? "'s worktree " + words.worktree : "") + ", run:"));
      for (const [cmd, what] of steps) sec.append(copyLine(cmd, what));
    }
    out.push(sec);
  }

  // About: the facts, as a person would put them.
  const facts = el("dl", "facts-list");
  const fact = (k, v) => { if (v) facts.append(el("dt", null, k), typeof v === "string" ? el("dd", null, v) : wrap("dd", v)); };
  const pl = el("a", "proj", p.org ? p.org + "/" + p.name : p.name);
  pl.href = "#project=" + encodeURIComponent(item.project);
  pl.addEventListener("click", () => { state.pushedOpen = false; });
  fact("Project", pl);
  fact("Source", FROM[item.source] || item.source);
  fact("Noted", item.since && ago(item.since) + " · " + when(item.since));
  if (item.reviewed_at) fact("Last checked", when(item.reviewed_at));
  if (ref) {
    if (ref.href) {
      const a = el("a", null, ref.word + " ↗");
      a.href = ref.href; a.target = "_blank"; a.rel = "noopener noreferrer";
      fact("Tracker", a);
    } else fact("Tracker", ref.word + (item.upstream && item.upstream.state ? " · " + item.upstream.state : ""));
  }
  if (item.stale) fact("Stale", "Not looked at in a while; sous review will ask if it still matters");
  if (p.remote) fact("Repository", siteLink(p, null, p.remote + " ↗") || p.remote);
  const about = el("div", "d-section");
  about.append(el("h3", null, "About"), facts);
  out.push(about);

  // A note no agent may take yet says how to allow it, where it matters.
  if (agentable && !canRun(item.project)) {
    const sec = el("div", "d-section");
    sec.append(el("h3", null, "Hand it to an agent"),
      el("p", "hint", "Agents can't start work in " + p.name + " from this page yet. Allow it once, in a terminal, and a Hand to an agent button appears here:"),
      copyLine(`sous config -p ${p.name} remote_runs true`, "Let agents start runs in " + p.name));
    out.push(sec);
  }

  // The same moves from a terminal, each saying what it does.
  const term = el("details", "d-section terminal");
  term.append(el("summary", null, "Do it from a terminal"));
  if (isNote) {
    term.append(copyLine(`sous show ${item.id}`, "Print this note in full"));
    if (has.has("done")) term.append(copyLine(`sous done ${item.id}`, "Mark it done"));
  }
  term.append(copyLine(`sous go ${p.name}`, "Open " + p.name + " in your shell and see what's waiting there"));
  out.push(term);

  if (words.title !== (item.text || "").trim() && !words.git) {
    const raw = el("details", "d-section raw");
    raw.append(el("summary", null, "As written"), el("p", "mono", item.text));
    out.push(raw);
  }
  return out;
}

function wrap(tag, child) { const e = el(tag); e.append(child); return e; }

// copyLine is a command and, when given, what it does: one tap copies it.
function copyLine(text, what) {
  const d = el("div", "copy");
  const line = el("div", "copy-line");
  line.append(el("code", null, text), button("Copy", "quiet small", async (e) => {
    try { await navigator.clipboard.writeText(text); e.target.textContent = "Copied"; }
    catch { e.target.textContent = "Select it"; }
    setTimeout(() => { e.target.textContent = "Copy"; }, 1500);
  }));
  if (what) d.append(el("span", "what", what));
  d.append(line);
  return d;
}

function runSection(item, run, s, has) {
  const sec = el("div", "d-section");
  sec.append(el("h3", null, "Agent run" + (run.runner ? " · " + run.runner : "")));
  if (s.id === "needs_you") sec.append(...questionParts(item, run, has));
  else if (s.id === "in_progress") { const st = el("div", "step"); st.append(...progressBits(run)); sec.append(st); }
  else if (s.id === "ready") sec.append(...resultParts(item, run, has));
  else if (run.text) sec.append(el("p", "muted", run.text));
  if (run.error) sec.append(el("p", "blocked", "Status unavailable: " + run.error));
  if (has.has("stop")) {
    const a = el("div", "actions");
    a.append(button("Stop", "quiet danger", () => confirmThen("Stop this run?", item.text, () => act("stop", item.id))));
    sec.append(a);
  }
  return sec;
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
    t.value = state.drafts[item.key] || "";
    t.setAttribute("aria-label", "Answer");
    t.addEventListener("input", () => { state.drafts[item.key] = t.value; });
    o.append(t, button("Send", "primary", () => {
      if (!t.value.trim()) return;
      send(t.value.trim());
      delete state.drafts[item.key];
    }));
    out.push(o);
  }
  return out;
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

function button(label, cls, onClick) {
  const b = el("button", cls, label);
  b.type = "button";
  b.addEventListener("click", onClick);
  return b;
}

// ---- Selecting and quick actions ----------------------------------------

function toggleSelect(key) {
  if (state.selected.has(key)) state.selected.delete(key); else state.selected.add(key);
  renderList();
}

function renderBulk() {
  for (const k of state.selected) if (!state.items.has(k)) state.selected.delete(k);
  const n = state.selected.size;
  $("bulk").hidden = !n;
  $("board").classList.toggle("selecting", n > 0);
  $("bulk-n").textContent = n + " selected";
  const items = [...state.selected].map((k) => state.items.get(k));
  $("bulk-done").disabled = !items.some((i) => (i.actions || []).includes("done"));
}

// quick does done or snooze at once: the rows fade, and leave when sous
// says so. What could not be done stays, with the reason.
async function quick(action, items) {
  const can = items.filter((i) => (i.actions || []).includes(action));
  if (!can.length) return;
  for (const i of can) { state.leaving.add(i.key); state.selected.delete(i.key); }
  if (state.open && can.some((i) => i.key === state.open)) {
    const next = state.order[state.order.indexOf(state.open) + 1] || null;
    state.cur = next;
    go({ open: null }, true);
  }
  renderList();
  let ok = 0;
  for (const i of can) {
    const out = await act(action, i.id, {}, true);
    if (out === "done") ok++;
    else state.leaving.delete(i.key);
  }
  renderList();
  const word = action === "done" ? "Marked done" : "Snoozed";
  if (ok) toast(can.length === 1 ? `${word}: ${can[0].text}` : `${word}: ${ok} of ${plural(can.length, "item")}`, "--ready");
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

async function act(action, target, extra = {}, quiet = false) {
  const body = { action, op: newOp(), target, ...extra };
  const p = pending();
  p[body.op] = body;
  savePending(p);
  return send(body, quiet);
}

// send returns "done" when sous did it (now or before), else what went wrong.
async function send(body, quiet) {
  let res, out;
  try {
    res = await fetch("/api/act", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    out = await res.json();
  } catch {
    toast("Not sent yet; it goes when sous is back.", "--needs");
    return "unsent";
  }
  // Done and snooze use ordinary CLI acknowledgements; run actions use
  // integration outcomes. Accept only the acknowledgement for this action.
  const acknowledged = !out.error && (
    (body.action === "done" && ["closed", "already_closed"].includes(out.did)) ||
    (body.action === "snooze" && out.did === "snoozed")
  );
  const outcome = out.outcome || (acknowledged ? "done" : undefined);
  if (outcome === "in_progress") return "in_progress"; // the first is still being done; ask again later
  const p = pending();
  delete p[body.op];
  savePending(p);
  const ok = outcome === "done" || outcome === "already_done";
  if (ok && quiet) return "done";
  const words = {
    done: "Done", already_done: "Done", refused: "Refused", conflict: "That changed in the meantime",
    failed: "Failed", not_ready: "sous is not set up for that",
  };
  const role = ok ? "--ready" : outcome === "conflict" ? "--needs" : "--failed";
  let msg = (words[outcome] || out.error || "Something went wrong") + " · " + body.action;
  if (out.reason) msg += ": " + out.reason.replace(/_/g, " ");
  if (out.reason === "not_allowed_project") msg += " (sous config -p <project> remote_runs true)";
  toast(msg, role);
  return ok ? "done" : outcome || "failed";
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

function openTask(project, brief) {
  const d = $("task-dialog");
  const sel = $("task-project");
  sel.replaceChildren();
  const paths = (state.snap && state.snap.run_projects) || [];
  for (const path of paths) {
    const p = projectOf(path);
    const o = el("option", null, p.org ? p.org + "/" + p.name : p.name);
    o.value = path;
    sel.append(o);
  }
  const here = project || state.project;
  if (here && paths.includes(here)) sel.value = here;
  if (brief) $("task-brief").value = brief;
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

// ---- Keys ---------------------------------------------------------------

function moveCur(step) {
  if (!state.order.length) return;
  const i = state.order.indexOf(state.cur || state.open);
  const next = state.order[Math.max(0, Math.min(state.order.length - 1, i < 0 ? 0 : i + step))];
  state.cur = next;
  if (state.open) go({ open: next }, true);
  else renderList();
  const r = document.querySelector(`.row[data-key="${CSS.escape(next)}"]`);
  if (r) r.scrollIntoView({ block: "nearest" });
}

function onKey(e) {
  if (e.metaKey || e.ctrlKey || e.altKey || document.querySelector("dialog[open]")) return;
  const typing = e.target.closest("input, textarea, select");
  if (e.key === "Escape") {
    if (typing) { e.target.blur(); return; }
    if (state.selected.size) { state.selected.clear(); renderList(); return; }
    if (state.open) closeItem();
    else $("side").classList.remove("shown");
    return;
  }
  if (typing) return;
  const cur = state.items.get(state.cur || state.open);
  const targets = () => state.selected.size ? [...state.selected].map((k) => state.items.get(k)) : cur ? [cur] : [];
  switch (e.key) {
    case "/": e.preventDefault(); $("search").focus(); $("search").select(); break;
    case "j": case "ArrowDown": e.preventDefault(); moveCur(1); break;
    case "k": case "ArrowUp": e.preventDefault(); moveCur(-1); break;
    case "Enter": case "o": if (state.cur) openItem(state.cur); break;
    case "x": if (state.cur) toggleSelect(state.cur); break;
    case "e": quick("done", targets()); break;
    case "s": quick("snooze", targets()); break;
    case "n": e.preventDefault(); openTask(); break;
    case "g": {
      const site = siteOf(projectOf(cur ? cur.project : state.project));
      if (site) window.open(site.href, "_blank", "noopener");
      break;
    }
  }
}

// ---- Start --------------------------------------------------------------

document.addEventListener("DOMContentLoaded", () => {
  for (const b of document.querySelectorAll("[data-close]")) b.addEventListener("click", () => b.closest("dialog").close());
  $("confirm-yes").value = "yes";
  $("new-task").addEventListener("click", () => openTask());
  $("view-run").addEventListener("click", () => openTask(state.project));
  $("menu").addEventListener("click", () => showSide(!$("side").classList.contains("shown")));
  $("scrim").addEventListener("click", () => { if ($("side").classList.contains("shown")) showSide(false); else if (state.open) closeItem(); });
  $("task-form").addEventListener("submit", (e) => {
    const brief = $("task-brief").value.trim();
    const project = $("task-project").value;
    if (!brief || !project) { e.preventDefault(); return; }
    act("run", project, { input: brief });
    $("task-brief").value = "";
  });
  $("search").addEventListener("input", (e) => go({ q: e.target.value.trim() }, true));
  $("project-filter").addEventListener("input", renderSide);
  $("group-by").addEventListener("change", (e) => { state.group[viewKey()] = e.target.value; savePrefs(); renderList(); });
  $("sort-by").addEventListener("change", (e) => { state.sort = e.target.value; savePrefs(); renderList(); });
  $("bulk-done").addEventListener("click", () => quick("done", [...state.selected].map((k) => state.items.get(k))));
  $("bulk-snooze").addEventListener("click", () => quick("snooze", [...state.selected].map((k) => state.items.get(k))));
  $("bulk-clear").addEventListener("click", () => { state.selected.clear(); renderList(); });
  document.addEventListener("keydown", onKey);
  window.addEventListener("popstate", () => { state.pushedOpen = false; readAddress(); render(); });
  window.addEventListener("hashchange", () => { readAddress(); render(); });
  window.addEventListener("resize", syncScrim);
  // A long press ends when the finger lifts, wherever the redrawn list put
  // it; the click that follows that lift is the press's, not a tap.
  document.addEventListener("pointerup", () => { if (state.held) { state.held = false; state.pressed = Date.now(); } }, true);
  document.addEventListener("pointercancel", () => { state.held = false; }, true);
  setInterval(() => { if (state.snap) renderList(); if (!state.live) setLive(false); }, 30000);
  loadPrefs();
  readAddress();
  loadTokens();
  connect();
});
