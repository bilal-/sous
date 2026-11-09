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
for (const [action, word] of [["file", "Filed"], ["link", "Linked"]]) {
  test(`${action} says ${word} when sous did it`, async () => {
    const x = make({ kind: "outcome", v: 1, outcome: "done" });
    const b = { ...body, action };
    x.data.set("sous.pending", JSON.stringify({ [b.op]: b }));
    assert.equal(await x.run(`send(${JSON.stringify(b)}, false)`), "done");
    assert.deepEqual(x.messages, [word]);
  });
}
test("a refused link says what sous can follow", async () => {
  const x = make({ kind: "outcome", v: 1, outcome: "refused", reason: "bad_input" });
  const b = { ...body, action: "link" };
  x.data.set("sous.pending", JSON.stringify({ [b.op]: b }));
  assert.equal(await x.run(`send(${JSON.stringify(b)}, false)`), "refused");
  assert.ok(x.messages.some((m) => String(m).includes("issue's address")), JSON.stringify(x.messages));
});
test("in_progress retains the same pending op", async () => {
  const x = await send({ outcome: "in_progress" }, "stop");
  assert.equal(x.result, "in_progress"); assert.deepEqual(Object.keys(x.pending), [body.op]);
});
test("transport failure retains the same pending op", async () => {
  const x = await send(null, "stop", true);
  assert.equal(x.result, "unsent"); assert.deepEqual(Object.keys(x.pending), [body.op]);
});
for (const status of [400,403,409,503]) {
  test(`pre-execution HTTP ${status} removes a pending action without JSON`, async () => {
    const x=make(null,false,{fetch:async()=>({status,ok:false,json:async()=>{throw Error("text")}})});
    x.data.set("sous.pending",JSON.stringify({[body.op]:body}));
    assert.equal(await x.run(`send(${JSON.stringify(body)})`),"refused");
    assert.deepEqual(JSON.parse(x.data.get("sous.pending")),{});
  });
}
for (const status of [500,502,504]) {
  test(`HTTP ${status} preserves uncertain delivery`, async()=>{
    const x=make(null,false,{fetch:async()=>({status,ok:false})});
    x.data.set("sous.pending",JSON.stringify({[body.op]:body}));
    assert.equal(await x.run(`send(${JSON.stringify(body)})`),"unsent");
    assert.deepEqual(Object.keys(JSON.parse(x.data.get("sous.pending"))),[body.op]);
  });
}
test("an offline click never enters the action queue",async()=>{
  const x=make({});
  assert.equal(await x.run('act("run","/tmp/example")'),"refused");
  assert.equal(x.calls.length,0);assert.equal(x.data.has("sous.pending"),false);
});
test("missing-cache explanation survives the status interval",()=>{
  const x=make({});x.run('setLive(false,"no board cache");setLive(false)');
  assert.equal(x.nodes.get("live").textContent,"no board cache");
});
test("fatal EventSource errors recreate one stream with bounded backoff",()=>{
  const streams=[];
  class Events {constructor(){streams.push(this);}addEventListener(){}close(){this.closed=true;}}
  const x=make({},false,{EventSource:Events});x.run("connect()");
  for(let i=0;i<8;i++){
    const es=streams.at(-1);es.onerror();es.onerror();
    assert.equal(es.closed,true);assert.equal(x.timers.size,1);
    const delay=x.nextTimer();assert.ok(delay>0&&delay<=15000);
  }
  assert.equal(streams.length,9);
});
test("empty-cache events offer Load board without fetching or queuing a build",()=>{
  const x=make({});x.run('state.connected=true;applyEvent({type:"unavailable",error:"no cache"})');
  assert.equal(x.calls.length,0);
  assert.equal(x.nodes.get("load-board").disabled,false);
});
test("explicit Load board reports a persistent failure without using pending ops",async()=>{
  const x=make({},false,{fetch:async(_url,opts)=>({ok:true,status:opts?.method==="POST"?202:200,
    json:async()=>opts?.method==="POST"?{loading:true}:{loading:false,error:"provider unavailable"}})});
  x.run("state.connected=true");await x.run("loadBoard()");
  assert.equal(x.calls[0][0],"/api/board-load");assert.equal(x.calls[0][1].method,"POST");
  assert.equal(x.data.has("sous.pending"),false);
  assert.match(x.nodes.get("board-load-status").textContent,/provider unavailable/);
  x.run("setLive(false)");assert.match(x.nodes.get("board-load-status").textContent,/provider unavailable/);
});
(async () => {
  let failed = 0;
  for (const [name, f] of cases) {
    try { await f(); console.log(`PASS ${name}`); }
    catch (err) { failed++; console.error(`FAIL ${name}: ${err.stack || err}`); }
  }
  if (failed) process.exitCode = 1;
})();
