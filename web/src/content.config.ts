import { defineCollection, z } from "astro:content";
import { glob } from "astro/loaders";
import { docsSchema } from "@astrojs/starlight/schema";

// Docs are read from the repository's canonical /docs directory rather
// than copied into src/. One source of truth: the same files browse on
// GitHub and build here, so a docs change is one PR, not two.
//
// IDs are prefixed with "docs/" to leave "/" free for the landing page,
// and README.md becomes its directory's index.
export const collections = {
  docs: defineCollection({
    loader: glob({
      base: "../docs",
      pattern: "**/*.{md,mdx}",
      generateId: ({ entry }) => {
        const path = entry
          .replace(/\.mdx?$/, "")
          .replace(/(^|\/)README$/, "")
          .replace(/\/$/, "");
        return path ? `docs/${path}` : "docs";
      },
    }),
    schema: (ctx) =>
      docsSchema({
        extend: z.object({
          // The repo's own frontmatter convention, carried through to
          // the page: summary becomes the lede, read_when the callout.
          summary: z.string().optional(),
          read_when: z.array(z.string()).optional(),
        }),
      })(ctx).transform((data) => ({
        ...data,
        description: data.description ?? data.summary,
      })),
  }),
};
