// Expressive Code lives in its own config file so the <Code> component
// can be rendered outside markdown (the landing page uses it for every
// snippet). Starlight picks this up automatically.
import { graphiteDark, graphiteLight } from "./src/shiki/graphite.js";

export default {
  // Theme type drives Starlight's default [data-theme='…'] selector, so
  // light/dark switch without extra wiring.
  themes: [graphiteLight, graphiteDark],
  styleOverrides: {
    borderRadius: "0px",
    borderColor: "var(--line-strong)",
    codeFontFamily: "var(--font-mono)",
    codeFontSize: "0.785rem",
    codeLineHeight: "1.72",
    uiFontFamily: "var(--font-mono)",
    frames: {
      shadowColor: "transparent",
      editorActiveTabIndicatorTopColor: "var(--warm)",
    },
    // Box-art figures mark their active node with a text marker. The
    // default derives a blue from the theme, which would be the first
    // off-palette colour on the site.
    textMarkers: {
      markBackground: "var(--warm-faint)",
      markBorderColor: "var(--warm)",
    },
  },
};
