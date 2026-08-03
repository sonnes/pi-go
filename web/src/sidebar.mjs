import { readdirSync } from "node:fs";

// Starlight's `autogenerate` matches on a file path under
// src/content/docs, which our loader doesn't use — docs live in the
// repository's /docs directory. So groups are built from that directory
// directly: add a markdown file, get a sidebar entry, no config edit.
//
// A learning progression rarely matches alphabetical order, so `order`
// names the pages that need placing. Anything unlisted still appears,
// appended alphabetically, which keeps "drop a file in and it shows up".
function slugsIn(dir, order = []) {
  const rank = (name) => order.indexOf(name) + 1 || Infinity;
  return readdirSync(new URL(`../../docs/${dir}/`, import.meta.url))
    .filter((f) => /\.mdx?$/.test(f) && !/^README\.mdx?$/.test(f))
    .map((f) => f.replace(/\.mdx?$/, ""))
    .sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
    .map((name) => ({ slug: `docs/${dir}/${name}` }));
}

export const sidebar = [
  {
    label: "Start here",
    items: [
      { slug: "docs", label: "Overview" },
      { slug: "docs/quickstart" },
    ],
  },
  {
    // Dependency order, not alphabetical: each page assumes only the
    // ones above it.
    label: "Mental models",
    items: slugsIn("concepts", [
      "how-a-run-works",
      "messages",
      "models",
      "providers",
      "catalog",
      "options",
      "prompt-caching",
      "tools",
      "streams",
      "agent-loop",
      "sessions",
      "transcript-tree",
      "harness",
      "oauth",
      "openrouter-dialect",
    ]),
  },
  {
    // Capability order: each guide assumes the ones above it.
    label: "Guides",
    items: slugsIn("guides", [
      "text-generation",
      "structured-output",
      "multimodal",
      "tools",
      "agents",
      "built-in-tools",
      "persistence",
      "conversation-editing",
      "file-configured-agents",
      "cli-agents",
      "authentication",
    ]),
  },
  {
    label: "Customization",
    items: [
      { slug: "docs/extend", label: "Which seam?" },
      ...slugsIn("extend", [
        "hooks",
        "harness",
        "providers",
        "storage",
        "agents",
      ]),
    ],
  },
  {
    // Top-down, mirroring the stack.
    label: "Layers",
    items: [
      { slug: "docs/layers", label: "Choosing a layer" },
      ...slugsIn("layers", [
        "front-door",
        "composition",
        "durability",
        "loop",
        "direct",
      ]),
    ],
  },
  {
    label: "Architecture",
    items: [
      { slug: "docs/architecture", label: "Overview" },
      ...slugsIn("architecture", [
        "ai",
        "providers",
        "agent",
        "catalog-and-pi",
        "session-and-durable",
        "harness",
        "tools-and-sandbox",
      ]),
    ],
  },
  {
    label: "Reference",
    items: [
      { slug: "docs/reference/glossary" },
      { slug: "docs/reference/modules" },
      { slug: "docs/reference/cli" },
      {
        label: "Provider capabilities",
        collapsed: true,
        items: [
          { slug: "docs/reference/capabilities", label: "Matrix overview" },
          ...slugsIn("reference/capabilities"),
        ],
      },
    ],
  },
];
