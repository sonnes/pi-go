// Smoke test for the demo module: loads the real .wasm, drives it the
// way the page does, and asserts the agent loop actually ran — text
// streamed, the tool dispatched, and the transcript tree came back in
// the expected shape.
//
// Run with `make site-wasm-test`.

import { readFile } from "node:fs/promises";
import { argv, exit } from "node:process";

const wasmPath = argv[2] ?? "web/public/pi-demo.wasm";
const execPath = argv[3];

if (!execPath) {
  console.error("usage: node wasm-smoke.mjs <module.wasm> <wasm_exec.js>");
  exit(2);
}

await import(execPath);

const go = new globalThis.Go();
// wasm_exec passes argv+env into the module and rejects anything large;
// the demo needs neither.
go.env = {};
go.argv = ["piwasm"];

const { instance } = await WebAssembly.instantiate(await readFile(wasmPath), go.importObject);

// main blocks forever on purpose — start it, don't await it.
go.run(instance);

if (typeof globalThis.piDemo?.start !== "function") {
  fail("module did not install globalThis.piDemo");
}

const events = [];
globalThis.piDemo.start((json) => events.push(JSON.parse(json)));
globalThis.piDemo.send(JSON.stringify({ kind: "run", text: "What's the weather in Paris?" }));

await waitFor(() => events.some((e) => e.kind === "done"), 10_000, "the run to finish");

const streamed = events
  .filter((e) => e.kind === "delta")
  .map((e) => e.text)
  .join("");
const tool = events.find((e) => e.kind === "tool" && e.tool?.result);
const tree = [...events].reverse().find((e) => e.kind === "tree")?.tree;

if (!streamed.trim()) fail("no text streamed");
if (!tool) fail("the tool never produced a result");
if (!tree?.length) fail("no transcript tree");

const roles = [];
for (let node = tree[0]; node; node = node.children?.[0]) roles.push(node.role);

const want = ["user", "assistant", "tool_result", "assistant"];
if (roles.join(",") !== want.join(",")) {
  fail(`transcript is ${roles.join(" → ")}, want ${want.join(" → ")}`);
}

console.log(`wasm smoke ok — streamed ${streamed.trim().length} chars`);
console.log(`  tool: ${tool.tool.name} → ${tool.tool.result}`);
console.log(`  tree: ${roles.join(" → ")}`);
exit(0);

function fail(message) {
  console.error(`wasm smoke FAILED: ${message}`);
  console.error(JSON.stringify(events, null, 2).slice(0, 2000));
  exit(1);
}

async function waitFor(predicate, timeoutMs, what) {
  const deadline = Date.now() + timeoutMs;
  while (!predicate()) {
    if (Date.now() > deadline) fail(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 25));
  }
}
