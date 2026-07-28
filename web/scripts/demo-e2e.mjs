// End-to-end check of the demo in a real browser, over the DevTools
// protocol so it needs no extra dependencies: click "Run it here", wait
// for the module to load, run a turn, then assert on what rendered.
//
// Local check, not CI — it needs a Chrome binary and a running preview
// server. `make site-wasm-test` is the headless equivalent that CI runs.
//
//   make site-build && (cd web && pnpm preview) &
//   node web/scripts/demo-e2e.mjs
import { spawn } from "node:child_process";

const CHROME =
  process.env.CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const URL = process.argv[2] ?? "http://localhost:4321/pi-go/";

const chrome = spawn(CHROME, [
  "--headless=new",
  "--disable-gpu",
  "--remote-debugging-port=9333",
  "--user-data-dir=/tmp/chrome-e2e",
  "about:blank",
]);

let ws, id = 0;
const pending = new Map();

try {
  await waitFor(async () => (await targets()).length > 0, 10000, "chrome");
  const target = (await targets()).find((t) => t.type === "page");
  ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((r) => (ws.onopen = r));
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (pending.has(msg.id)) pending.get(msg.id)(msg.result);
  };

  await send("Page.enable");
  await send("Runtime.enable");
  await send("Page.navigate", { url: URL });
  await new Promise((r) => setTimeout(r, 2500));

  const started = await evaluate(`
    (() => {
      const b = document.querySelector('[data-start]');
      if (!b) return 'no start button';
      b.click();
      return 'clicked';
    })()
  `);
  console.log("start:", started);

  await waitFor(
    async () => (await evaluate(`document.querySelector('[data-out]')?.textContent ?? ''`)).includes("ready"),
    30000,
    "the module to load",
  );

  await evaluate(`document.querySelector('[data-cmd="run"]').click()`);
  await waitFor(
    async () => (await evaluate(`document.querySelector('[data-tree]').children.length`)) >= 4,
    30000,
    "the run to produce a transcript",
  );

  const out = await evaluate(`document.querySelector('[data-out]').textContent`);
  const tree = await evaluate(
    `[...document.querySelectorAll('[data-tree] button')].map(b => b.textContent.trim()).join('\\n')`,
  );
  const mode = await evaluate(`document.querySelector('[data-mode]').textContent`);
  const staticHidden = await evaluate(
    `document.querySelector('[data-static-tree]')?.hasAttribute('hidden')`,
  );

  console.log("\n--- terminal ---\n" + out.trim());
  console.log("\n--- tree ---\n" + tree);
  console.log("\nmode:", mode, "| static diagram hidden:", staticHidden);

  const ok = out.includes("22°C") && tree.includes("tool_result");
  console.log(ok ? "\nE2E OK" : "\nE2E FAILED");
  process.exit(ok ? 0 : 1);
} finally {
  ws?.close();
  chrome.kill();
}

function send(method, params = {}) {
  return new Promise((resolve) => {
    const msgId = ++id;
    pending.set(msgId, resolve);
    ws.send(JSON.stringify({ id: msgId, method, params }));
  });
}

async function evaluate(expression) {
  const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
  return r?.result?.value;
}

async function targets() {
  try {
    return await (await fetch("http://localhost:9333/json")).json();
  } catch {
    return [];
  }
}

async function waitFor(predicate, timeoutMs, what) {
  const deadline = Date.now() + timeoutMs;
  while (!(await predicate())) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 200));
  }
}
