"use strict";
const assert = require("node:assert/strict");
const { make } = require("./app_harness.cjs");
const cases = [];
function test(name, f) { cases.push([name, f]); }
function prepareDetail(x) {
  x.run(`state.snap = { projects: [{ path: '/code/acme/api', name: 'api', org: 'acme' }], run_projects: [] };
    state.open = 'note:7'; state.drafts['note:7'] = 'unfinished answer';
    state.items.set('note:7', { key: 'note:7', id: '7', project: '/code/acme/api', text: 'Check the build', section: 'on_you', source: 'human', actions: [] });`);
}
test("selected detail remains valid when note becomes snoozed", () => {
  const x = make({}); prepareDetail(x); x.run("renderDetail()");
  const before = x.nodes.get("detail").replacements;
  x.run("renderDetail()"); assert.equal(x.nodes.get("detail").replacements, before);
  x.run("state.items.get('note:7').section = 'snoozed'; renderDetail()");
  assert.equal(x.nodes.get("detail").hidden, false);
  assert.ok(descendants(x.nodes.get("detail")).some(n => n.textContent === "snoozed"));
  assert.equal(x.run("state.drafts['note:7']"), "unfinished answer");
  assert.equal(x.run("statusOf(state.items.get('note:7'))"), null); // no new board section
});
test("bookmarked snoozed detail renders without a previous visible item", () => {
  const x = make({}); prepareDetail(x);
  x.run("state.items.get('note:7').section = 'snoozed'; renderDetail()");
  assert.equal(x.nodes.get("detail").hidden, false);
  assert.ok(descendants(x.nodes.get("detail")).some(n => n.textContent === "snoozed"));
});
function descendants(node) {
  return [node, ...(node.children || []).flatMap(descendants)];
}
test("snoozed run keeps its question controls and draft", () => {
  const x = make({}); prepareDetail(x);
  x.run(`const item = state.items.get('note:7');
    item.section = 'snoozed'; item.actions = ['reply'];
    item.run = { phase: 'needs_you', state: 'needs_you', question: { id: 'q1', token: 'q-token', text: 'Which endpoint?', other: true } };
    renderDetail();`);
  assert.equal(x.run("statusOf(state.items.get('note:7'))"), "needs_you");
  const input = descendants(x.nodes.get("detail")).find(n => n.tagName === "textarea");
  assert.ok(input); assert.equal(input.value, "unfinished answer");
  const before = x.nodes.get("detail").replacements;
  x.run("renderDetail()");
  assert.equal(x.nodes.get("detail").replacements, before);
});
(async () => {
  let failed = 0;
  for (const [name, f] of cases) {
    try { await f(); console.log(`PASS ${name}`); }
    catch (err) { failed++; console.error(`FAIL ${name}: ${err.stack || err}`); }
  }
  if (failed) process.exitCode = 1;
})();
