// Docs links must resolve in both places the docs are read: as markdown
// on GitHub, and as pages on the site. One form satisfies both — a
// repo-relative path to the target file — and web/src/remark/
// rewrite-doc-links.mjs turns it into a route at build time.
//
// This checks the two things that plugin cannot fix for you: that no link
// is site-absolute, and that every relative target exists on disk.
//
//   node web/scripts/check-links.mjs [docsDir]

import { readdirSync, readFileSync, existsSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";

const docsDir = resolve(process.argv[2] ?? "docs");
const EXTERNAL = /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i;

function walk(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (e.isDirectory()) return walk(p);
    return /\.mdx?$/.test(e.name) ? [p] : [];
  });
}

// Fenced blocks hold Go, and Go generics like fs.New[T]("./x") look
// exactly like a markdown link. Blank them before scanning.
function stripCode(text) {
  return text
    .replace(/^```[\s\S]*?^```/gm, "")
    .replace(/`[^`\n]*`/g, "");
}

const problems = [];

for (const file of walk(docsDir)) {
  const body = stripCode(readFileSync(file, "utf8"));
  const rel = relative(process.cwd(), file);

  for (const [, url] of body.matchAll(/\]\(([^)\s]+)\)/g)) {
    if (EXTERNAL.test(url)) continue;

    if (url.startsWith("/")) {
      problems.push(`${rel}: site-absolute link ${url}`);
      continue;
    }

    const target = resolve(dirname(file), url.split("#")[0].split("?")[0]);
    if (!existsSync(target)) {
      problems.push(`${rel}: dead link ${url}`);
    }
  }
}

if (problems.length > 0) {
  console.error(problems.join("\n"));
  console.error(
    `\n${problems.length} problem(s). Internal links must be repo-relative ` +
      `paths to an existing file, e.g. ../concepts/ai/tools.md`,
  );
  process.exit(1);
}

console.log("docs links ok");
