"use strict";
// present() turns an item's terse text into what a person reads first.
const assert = require("node:assert/strict");
const { make } = require("./app_harness.cjs");
const x = make({});
const present = (item) => JSON.parse(x.run(`JSON.stringify(present(${JSON.stringify(item)}))`));
const cases = [];
function test(name, f) { cases.push([name, f]); }

test("git: uncommitted files in a worktree", () => {
  const w = present({ source: "git", text: "2 files uncommitted · feature/x · worktree api-x" });
  assert.equal(w.title, "2 uncommitted files"); assert.equal(w.branch, "feature/x"); assert.equal(w.worktree, "api-x");
  assert.match(w.means, /aren't committed/);
});
test("git: one commit, one stash, a branch never pushed", () => {
  assert.equal(present({ source: "git", text: "1 commits unpushed · main" }).title, "1 commit not pushed");
  assert.equal(present({ source: "git", text: "1 stashes" }).title, "1 stashed change");
  const n = present({ source: "git", text: "feat/y has no upstream" });
  assert.equal(n.title, "Branch not tracking a remote"); assert.doesNotMatch(n.means, /only on this computer/); assert.equal(n.branch, "feat/y");
});
test("git: anything else is shown as written", () => {
  assert.equal(present({ source: "git", text: "something new" }).title, "something new");
});
test("tracker: label and number become the kicker", () => {
  const w = present({ source: "github", text: "review requested · PR #10 Bump the lockfile" });
  assert.equal(w.kicker, "Review requested · PR #10"); assert.equal(w.title, "Bump the lockfile"); assert.match(w.means, /review/);
  assert.equal(present({ source: "gitlab", text: "awaiting review · MR !3 Add fields" }).kicker, "Awaiting review · MR !3");
});
test("note: a short label becomes the kicker, the title is capitalised", () => {
  const w = present({ source: "human", text: "brand: decide on the wordmark weight" });
  assert.equal(w.kicker, "Brand"); assert.equal(w.title, "Decide on the wordmark weight");
});
test("note: a long note splits after its first sentence, not inside e.g.", () => {
  const text = "Check the build on the release branch before Friday. It failed twice last week, e.g. On the arm runner, and nobody looked into why it happened.";
  const w = present({ source: "human", text });
  assert.equal(w.title, "Check the build on the release branch before Friday.");
  assert.match(w.body, /^It failed twice/);
  const eg = present({ source: "human", text: "Look at the slow pages on the site, e.g. The checkout and the search results, and write down what each one loads first." });
  assert.equal(eg.body, "");
});
test("note: a short note stays whole; a URL is no kicker", () => {
  assert.deepEqual(present({ source: "human", text: "Call Sam" }), { kicker: "", title: "Call Sam", body: "" });
  assert.equal(present({ source: "human", text: "https://git.example.org/acme/api is down" }).kicker, "");
});

test("note: a kicker keeps the rest's own case when it starts with code", () => {
  const w = present({ source: "human", text: "architecture: tracker.Client{Home, Cache, Env} made in cli and passed down" });
  assert.equal(w.kicker, "Architecture"); assert.equal(w.title, "tracker.Client{Home, Cache, Env} made in cli and passed down");
  assert.equal(present({ source: "human", text: "harness: pi (TypeScript extension)" }).title, "pi (TypeScript extension)");
  assert.equal(present({ source: "human", text: "brand: decide, keep or drop" }).title, "Decide, keep or drop");
});
test("tracker items say where the kicker came from; notes do not", () => {
  assert.equal(present({ source: "github", text: "review requested · PR #10 Bump" }).tracker, true);
  assert.ok(!present({ source: "human", text: "harness: Cursor hooks", ref: "md:FOLLOWUPS.md:0743b7f5d680" }).tracker);
});
test("shellArg quotes what a shell would split or run, like text.ShellArg", () => {
  const q = (s) => x.run(`shellArg(${JSON.stringify(s)})`);
  assert.equal(q("feat/x-1.2"), "feat/x-1.2");
  assert.equal(q("acme/api"), "acme/api");
  assert.equal(q("My Project"), "'My Project'");
  assert.equal(q("feat;rm"), "'feat;rm'");
  assert.equal(q("it's"), "'it'\"'\"'s'");
  assert.equal(q(""), "''");
});

(async () => {
  let failed = 0;
  for (const [name, f] of cases) {
    try { await f(); console.log(`PASS ${name}`); }
    catch (err) { failed++; console.error(`FAIL ${name}: ${err.stack || err}`); }
  }
  if (failed) process.exitCode = 1;
})();
