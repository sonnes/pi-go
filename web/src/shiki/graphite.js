// Graphite syntax themes.
//
// The palette rule the site follows: graphite draws the structure, one
// rust signal marks what matters, green confirms. Code colours are
// derived from those tokens rather than picked separately, so a palette
// change never turns into a re-theme.

const light = {
  bg: "#ffffff",
  fg: "#16181a",
  comment: "#5f6669",
  string: "#96401f",
  keyword: "#33393d",
  type: "#2f6b45",
};

const dark = {
  bg: "#1b1e20",
  fg: "#e6e8e8",
  comment: "#969c9f",
  string: "#e59b72",
  keyword: "#c3cacd",
  type: "#6dbd8a",
};

function theme(name, type, c) {
  return {
    name,
    type,
    colors: {
      "editor.background": c.bg,
      "editor.foreground": c.fg,
    },
    settings: [
      {
        scope: ["comment", "punctuation.definition.comment"],
        settings: { foreground: c.comment, fontStyle: "italic" },
      },
      {
        scope: [
          "string",
          "string.quoted",
          "constant.other.symbol",
          "punctuation.definition.string",
        ],
        settings: { foreground: c.string },
      },
      {
        scope: [
          "keyword",
          "keyword.control",
          "storage",
          "storage.type",
          "storage.modifier",
          "variable.language",
          "keyword.operator.new",
        ],
        settings: { foreground: c.keyword, fontStyle: "bold" },
      },
      {
        scope: [
          "entity.name.type",
          "entity.name.class",
          "entity.name.struct",
          "support.type",
          "support.class",
          "constant.numeric",
          "constant.language",
        ],
        settings: { foreground: c.type },
      },
      {
        scope: ["entity.name.function", "support.function", "variable"],
        settings: { foreground: c.fg },
      },
    ],
  };
}

export const graphiteLight = theme("graphite-light", "light", light);
export const graphiteDark = theme("graphite-dark", "dark", dark);
