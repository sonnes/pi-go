// @ts-check
import { fileURLToPath } from "node:url";

import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { unified } from "@astrojs/markdown-remark";

import { stripTitleHeading } from "./src/remark/strip-title-heading.mjs";
import { rewriteDocLinks } from "./src/remark/rewrite-doc-links.mjs";
import { redirects } from "./src/redirects.mjs";
import { sidebar } from "./src/sidebar.mjs";

// Project-site defaults for GitHub Pages. With a custom domain, set
// base to "/" and site to the domain.
const BASE = "/pi-go";
const REPO = "https://github.com/sonnes/pi-go";

export default defineConfig({
  site: "https://sonnes.github.io",
  base: BASE,
  trailingSlash: "always",
  redirects,
  vite: {
    resolve: {
      // Docs live in /docs, outside this project root, so a relative
      // import out of an .mdx page would climb past it. The alias keeps
      // the resolved path inside web/ and the authored import short.
      alias: {
        "@diagrams": fileURLToPath(
          new URL("./src/components/diagrams", import.meta.url),
        ),
      },
    },
  },
  markdown: {
    // MDX inherits these off the shared processor, so both extensions
    // get the same treatment. stripTitleHeading only fires on the first
    // non-frontmatter node, so an `import` must never precede the H1.
    processor: unified({
      remarkPlugins: [
        stripTitleHeading,
        [rewriteDocLinks, { base: BASE, repo: REPO }],
      ],
    }),
  },
  integrations: [
    starlight({
      title: "pi-go",
      description:
        "A provider-agnostic SDK for building AI agents in Go — from a single completion call to durable, session-backed agents.",
      customCss: ["./src/styles/tokens.css", "./src/styles/starlight.css"],
      components: {
        // Renders summary as a lede and read_when as a callout.
        PageTitle: "./src/components/PageTitle.astro",
      },
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: REPO,
        },
      ],
      editLink: {
        baseUrl: `${REPO}/edit/main/`,
      },
      // Expressive Code options live in ec.config.mjs — required for the
      // <Code> component to render outside markdown.
      sidebar,
    }),
  ],
});
