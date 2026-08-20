import { readdirSync } from "node:fs";

// Starlight's `autogenerate` matches on a file path under
// src/content/docs, which our loader doesn't use — docs live in the
// repository's /docs directory. So groups are built from that directory
// directly: add a markdown file, get a sidebar entry, no config edit.
//
// A learning progression rarely matches alphabetical order, so `order`
// names the pages that need placing. Anything unlisted still appears,
// appended alphabetically, which keeps "drop a file in and it shows up".
//
// A missing directory yields an empty group rather than a build failure.
// The docs tree gets reorganised more often than this file does, and a
// stale name here must not be able to take the whole site down.
function slugsIn(dir, order = []) {
  const rank = (name) => order.indexOf(name) + 1 || Infinity;
  let files;
  try {
    files = readdirSync(new URL(`../../docs/${dir}/`, import.meta.url));
  } catch {
    return [];
  }
  return files
    .filter((f) => /\.mdx?$/.test(f) && !/^README\.mdx?$/.test(f))
    .map((f) => f.replace(/\.mdx?$/, ""))
    .sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
    .map((name) => ({ slug: `docs/${dir}/${name}` }));
}

export const sidebar = [
  {
    label: "Start here",
    items: [{ slug: "docs", label: "Overview" }],
  },
  {
    // Dependency order, not alphabetical: each page assumes only the
    // ones above it.
    label: "Concepts · ai",
    items: slugsIn("concepts/ai", [
      "content",
      "messages",
      "models",
      "providers",
      "options",
      "tools",
      "caching",
      "usage",
      "openrouter-dialect",
    ]),
  },
  {
    label: "Concepts · agent",
    items: slugsIn("concepts/agent", [
      "agent",
      "agent-state",
      "messages",
      "streaming",
    ]),
  },
  {
    label: "Concepts · durable",
    items: slugsIn("concepts/durable", ["sessions", "events", "entries", "tree"]),
  },
  {
    label: "Concepts · auth",
    items: slugsIn("concepts/auth", ["oauth"]),
  },
  {
    // Capability order: the features most people reach for come first.
    label: "Capabilities",
    items: [
      { slug: "docs/capabilities", label: "Matrix overview" },
      ...slugsIn("capabilities", [
        "function-calling",
        "streaming",
        "structured-outputs",
        "prompt-caching",
        "reasoning",
        "multimodal-input",
        "multimodal-output",
        "sampling",
        "usage-and-tokens",
        "citations",
        "server-tools",
        "stateful-conversations",
        "files-api",
        "async-execution",
        "realtime-api",
        "fine-tuning",
        "auth",
      ]),
    ],
  },
];
