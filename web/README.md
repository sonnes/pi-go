# pi-go site

Astro + Starlight. The landing page is hand-built; the documentation
pages are the repository's own `/docs` directory, read at build time.

```
make site-install   # pnpm install
make site-dev       # dev server with hot reload
make site-build     # static build to web/dist
make site-check     # astro check
```

## How docs get here

`src/content.config.ts` points Astro's `glob()` loader at `../docs`
instead of copying files into `src/content/docs`. One source of truth:
the same markdown browses on GitHub and builds here, so a docs change is
one commit rather than two.

Two conventions in the repo's frontmatter are carried through:

- `summary` becomes the page description and the lede under the title.
- `read_when` becomes the "Read this when" callout.

Both are wired in `src/components/PageTitle.astro`, which overrides
Starlight's own. IDs are prefixed with `docs/` so `/` stays free for the
landing page, and `README.md` becomes its directory's index.

Each doc opens with an `# H1` that repeats its title — right when the
file is read on GitHub, a duplicate once Starlight renders the title
itself. `src/remark/strip-title-heading.mjs` drops that first heading.

## The browser demo

Section 05 carries a live demo: the **real agent loop, typed tool
dispatch and durable session tree** compiled to WebAssembly and running
in the reader's browser. `cmd/piwasm` is the module; everything it does
beyond the `syscall/js` bridge is ordinary Go under
`cmd/piwasm/internal/demo`, covered by ordinary tests.

Three tiers, each degrading to the one below:

1. **No JavaScript** — the static entry-tree diagram, as before.
2. **Scripted** — one click fetches the module and runs the loop against
   a canned provider. No credentials, no network, deterministic output.
   Click any entry in the tree to rewind there and watch the next run
   grow a sibling.
3. **Live** — optionally swap the scripted provider for OpenRouter,
   using either the PKCE flow (no client secret and no registration
   required, so it works from a static page) or a pasted key. The key
   lives in `sessionStorage` for the tab and goes only into the wasm
   module; the default model is free.

```
make site-wasm        # build the module + copy the matching wasm_exec.js
make site-wasm-test   # drive it under Node and assert the loop ran
node web/scripts/demo-e2e.mjs   # same, in a real browser (local only)
```

`site-build` depends on `site-wasm`, and the module is generated rather
than committed. The Makefile enforces a size budget so it cannot quietly
grow past what a landing page should ask a visitor to download.

## Design system

Blueprint layout, Graphite palette, light-first, in
`src/styles/tokens.css`:

- The ruled ground is a drafting table and is never behind text. Every
  text-bearing region sits on an opaque sheet with an edge.
- Graphite `#33393d` draws the structure — sheet ticks, frame stripes,
  keywords, primary buttons. Rust `#b0492a` is the only accent, and the
  only link colour. Green is reserved for confirmation.
- JetBrains Mono for every heading, label and code span; Inter for prose.

`src/shiki/graphite.js` derives the syntax themes from those same tokens,
so a palette change does not become a re-theme. They are registered in
`ec.config.mjs` rather than `astro.config.mjs` because the `<Code>`
component cannot read options that live in the Astro config.

## Deployment

`.github/workflows/site.yml` builds and deploys to GitHub Pages on pushes
to `main` that touch `web/` or `docs/`. `astro.config.mjs` is set up for
a project site (`base: "/pi-go"`); with a custom domain, set `base` to
`"/"` and `site` to the domain.
