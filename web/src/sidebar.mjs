import { readdirSync } from "node:fs";

// Starlight's `autogenerate` matches on a file path under
// src/content/docs, which our loader doesn't use — docs live in the
// repository's /docs directory. So groups are built from that directory
// directly: add a markdown file, get a sidebar entry, no config edit.
function slugsIn(dir) {
  return readdirSync(new URL(`../../docs/${dir}/`, import.meta.url))
    .filter((f) => f.endsWith(".md") && f !== "README.md")
    .sort()
    .map((f) => ({ slug: `docs/${dir}/${f.replace(/\.md$/, "")}` }));
}

export const sidebar = [
  {
    label: "Start here",
    items: [{ slug: "docs", label: "Overview" }],
  },
  {
    label: "Concepts · ai",
    items: slugsIn("concepts/ai"),
  },
  {
    label: "Concepts · agent",
    items: slugsIn("concepts/agent"),
  },
  {
    label: "Concepts · auth",
    items: slugsIn("concepts/auth"),
  },
  {
    label: "Capabilities",
    items: [
      { slug: "docs/capabilities", label: "Matrix overview" },
      ...slugsIn("capabilities"),
    ],
  },
];
