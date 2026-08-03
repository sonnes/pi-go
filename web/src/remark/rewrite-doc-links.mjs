import { dirname, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Docs in /docs are read twice: as markdown on GitHub, and as pages on
 * this site. Only one link form is correct in both places — a plain
 * repo-relative path, the way any markdown file links to its neighbour:
 *
 *     [Entries](../durable/entries.md)      a sibling doc
 *     [tool.go](../../pkg/ai/tool.go#L163)  a source file
 *
 * GitHub resolves both. This plugin rewrites them for the site, where
 * neither resolves: doc targets become base-aware routes, and everything
 * else becomes an absolute link into the repository on GitHub.
 *
 * Authors therefore never write a site route by hand. A link that works
 * when you click it on GitHub is a link that works on the site.
 */

const DOCS_DIR = fileURLToPath(new URL("../../../docs/", import.meta.url));
const REPO_DIR = fileURLToPath(new URL("../../../", import.meta.url));

const EXTERNAL = /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i;
const DOC_EXT = /\.mdx?$/;

// Trailing "/README" is a directory index, and the docs collection maps
// /docs/README.md to the bare "docs" id (see web/src/content.config.ts).
function slugFor(absPath) {
  const rel = relative(DOCS_DIR, absPath).split(sep).join("/");
  const noExt = rel.replace(DOC_EXT, "");
  const noIndex = noExt.replace(/(^|\/)README$/, "");
  return noIndex.replace(/\/$/, "");
}

function isInside(dir, absPath) {
  const rel = relative(dir, absPath);
  return rel !== "" && !rel.startsWith("..") && !rel.startsWith(sep);
}

function walk(node, visit) {
  if (!node || typeof node !== "object") return;
  if (Array.isArray(node)) {
    for (const child of node) walk(child, visit);
    return;
  }
  if (node.type === "link") visit(node);
  // definition nodes carry the url of a reference-style link
  if (node.type === "definition") visit(node);
  if (Array.isArray(node.children)) walk(node.children, visit);
}

/**
 * @param {object} options
 * @param {string} options.base   Astro's `base`, e.g. "/pi-go".
 * @param {string} options.repo   Repository URL, no trailing slash.
 * @param {string} [options.branch]
 */
export function rewriteDocLinks({ base, repo, branch = "main" }) {
  const prefix = base === "/" ? "" : base.replace(/\/$/, "");

  return (tree, file) => {
    const from = file?.path ?? file?.history?.[0];
    if (!from) return;
    const fromDir = dirname(from);

    walk(tree, (node) => {
      const url = node.url;
      if (typeof url !== "string" || url === "" || EXTERNAL.test(url)) return;

      const [pathPart, ...rest] = url.split(/(?=[#?])/);
      const suffix = rest.join("");
      if (pathPart === "") return;

      const target = resolve(fromDir, pathPart);

      if (DOC_EXT.test(pathPart) && isInside(DOCS_DIR, target)) {
        const slug = slugFor(target);
        node.url = `${prefix}/${slug ? `docs/${slug}` : "docs"}/${suffix}`;
        return;
      }

      if (isInside(REPO_DIR, target)) {
        const rel = relative(REPO_DIR, target).split(sep).join("/");
        node.url = `${repo}/blob/${branch}/${rel}${suffix}`;
      }
    });
  };
}
