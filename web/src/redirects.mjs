// Every path the docs have ever been published at keeps working. The
// restructure moved pages out of per-package concept folders and into a
// reading order, so each old route points at wherever its content went.
//
// Keys are relative to `base` (they become output paths under dist/), but
// values are emitted into the redirect verbatim — Astro does not prefix
// them. So the target needs the base spelled out or the redirect lands
// outside the project site.
const BASE = "/pi-go";

const CAPABILITY_PAGES = [
  "async-execution",
  "auth",
  "citations",
  "files-api",
  "fine-tuning",
  "function-calling",
  "multimodal-input",
  "multimodal-output",
  "prompt-caching",
  "realtime-api",
  "reasoning",
  "sampling",
  "server-tools",
  "stateful-conversations",
  "streaming",
  "structured-outputs",
  "usage-and-tokens",
];

// The capability matrices became reference material rather than a
// top-level section; the pages themselves are unchanged.
const capabilities = Object.fromEntries([
  ["/docs/capabilities/", `${BASE}/docs/reference/capabilities/`],
  ...CAPABILITY_PAGES.map((p) => [
    `/docs/capabilities/${p}/`,
    `${BASE}/docs/reference/capabilities/${p}/`,
  ]),
]);

// Concepts were nested one directory per package and are now one flat
// reading order, with several small pages merged into their neighbours.
const CONCEPTS = {
  "ai/messages": "messages",
  "ai/content": "messages",
  "ai/models": "models",
  "ai/usage": "models",
  "ai/providers": "providers",
  "ai/options": "options",
  "ai/caching": "prompt-caching",
  "ai/tools": "tools",
  "ai/openrouter-dialect": "openrouter-dialect",
  "agent/agent": "agent-loop",
  "agent/messages": "agent-loop",
  "agent/agent-state": "agent-loop",
  "agent/streaming": "streams",
  "durable/sessions": "sessions",
  "durable/entries": "sessions",
  "durable/tree": "transcript-tree",
  "auth/oauth": "oauth",
  "harness/harness": "harness",
};

const concepts = Object.fromEntries(
  Object.entries(CONCEPTS).map(([from, to]) => [
    `/docs/concepts/${from}/`,
    `${BASE}/docs/concepts/${to}/`,
  ]),
);

export const redirects = {
  ...capabilities,
  ...concepts,
};
