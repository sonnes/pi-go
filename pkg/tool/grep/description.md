Search file contents for a pattern. Returns matching lines with file paths and line numbers.

- Supports full regex syntax (e.g. `log.*Error`, `function\s+\w+`). Set `literal: true` to match the pattern as a plain string instead.
- Filter files with the `glob` parameter (e.g. `*.js`, `**/*.tsx`).
- `ignore_case` for case-insensitive matching; `context` shows N lines before and after each match.
- `limit` caps the number of matching lines returned (default 100).
- Pattern syntax is RE2 (ripgrep) — no lookaround or backreferences.
