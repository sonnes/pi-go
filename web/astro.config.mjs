// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { unified } from "@astrojs/markdown-remark";

import { stripTitleHeading } from "./src/remark/strip-title-heading.mjs";
import { sidebar } from "./src/sidebar.mjs";

// Project-site defaults for GitHub Pages. With a custom domain, set
// base to "/" and site to the domain.
export default defineConfig({
  site: "https://sonnes.github.io",
  base: "/pi-go",
  trailingSlash: "always",
  markdown: {
    processor: unified({ remarkPlugins: [stripTitleHeading] }),
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
          href: "https://github.com/sonnes/pi-go",
        },
      ],
      editLink: {
        baseUrl: "https://github.com/sonnes/pi-go/edit/main/",
      },
      // Expressive Code options live in ec.config.mjs — required for the
      // <Code> component to render outside markdown.
      sidebar,
    }),
  ],
});
