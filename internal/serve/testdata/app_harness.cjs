"use strict";
// Run actual embedded app.js supplied by the Go wrapper. The DOM below is a
// deliberately small unit-test seam; this is not a browser/visual smoke test.
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync(process.argv[2], "utf8");
function element(tag = "div") {
  return { tagName: tag, children: [], hidden: false, textContent: "", value: "",
    style: { setProperty() {} }, classList: { toggle() {}, add() {}, remove() {} },
    append(...xs) { this.children.push(...xs); },
    replaceChildren(...xs) { this.children = xs; this.replacements = (this.replacements || 0) + 1; },
    setAttribute() {}, addEventListener() {}, remove() {}, focus() {}, scrollIntoView() {},
    get childElementCount() { return this.children.length; },
  };
}
function make(out, rejected = false) {
  const nodes = new Map(), data = new Map(), messages = [];
  const document = { addEventListener() {}, createElement: element,
    getElementById(id) { if (!nodes.has(id)) nodes.set(id, element()); return nodes.get(id); },
    querySelector() { return document.getElementById("app"); },
  };
  const context = vm.createContext({ document, console, URLSearchParams,
    localStorage: { getItem: k => data.get(k) || null, setItem: (k, v) => data.set(k, v) },
    setTimeout() {}, fetch: async () => { if (rejected) throw Error("offline"); return { json: async () => out }; },
    record: x => messages.push(x),
  });
  vm.runInContext(source, context);
  vm.runInContext("toast = text => record(text)", context);
  return { run: s => vm.runInContext(s, context), data, nodes, messages };
}

module.exports = { make };
