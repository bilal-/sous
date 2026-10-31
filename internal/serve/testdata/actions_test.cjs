"use strict";
const assert = require("node:assert/strict");
const { make } = require("./app_harness.cjs");
const cases = [];
function test(name, f) { cases.push([name, f]); }
const body = { action: "done", target: "7", op: "op-0123456789abcdef" };
async function send(out, action = "done", rejected = false) {
  const x = make(out, rejected), b = { ...body, action };
  x.data.set("sous.pending", JSON.stringify({ [b.op]: b }));
  const result = await x.run(`send(${JSON.stringify(b)}, true)`);
  return { ...x, result, pending: JSON.parse(x.data.get("sous.pending")) };
}
for (const [action, did] of [["done", "closed"], ["done", "already_closed"], ["snooze", "snoozed"]]) {
  test(`${action} acknowledges ${did}`, async () => {
    const x = await send({ id: "7", did, next: ["sous"] }, action);
    assert.equal(x.result, "done"); assert.deepEqual(x.pending, {}); assert.deepEqual(x.messages, []);
  });
}
for (const outcome of ["done", "already_done"]) {
  test(`v1 ${outcome} remains successful`, async () => {
    const x = await send({ kind: "outcome", v: 1, outcome }, "stop");
    assert.equal(x.result, "done"); assert.deepEqual(x.pending, {});
  });
}
for (const out of [{}, { did: "snoozed" }, { did: "closed", error: "write failed" }, { error: "not found" }]) {
  test(`unknown or failed ordinary response is not success: ${JSON.stringify(out)}`, async () => {
    const x = await send(out); assert.notEqual(x.result, "done"); assert.deepEqual(x.pending, {});
  });
}
for (const outcome of ["failed", "conflict", "refused", "not_ready"]) {
  test(`v1 ${outcome} is terminal and removes pending op`, async () => {
    const x = await send({ kind: "outcome", v: 1, outcome }, "stop");
    assert.equal(x.result, outcome); assert.deepEqual(x.pending, {});
  });
}
test("in_progress retains the same pending op", async () => {
  const x = await send({ outcome: "in_progress" }, "stop");
  assert.equal(x.result, "in_progress"); assert.deepEqual(Object.keys(x.pending), [body.op]);
});
test("transport failure retains the same pending op", async () => {
  const x = await send(null, "stop", true);
  assert.equal(x.result, "unsent"); assert.deepEqual(Object.keys(x.pending), [body.op]);
});
(async () => {
  let failed = 0;
  for (const [name, f] of cases) {
    try { await f(); console.log(`PASS ${name}`); }
    catch (err) { failed++; console.error(`FAIL ${name}: ${err.stack || err}`); }
  }
  if (failed) process.exitCode = 1;
})();
