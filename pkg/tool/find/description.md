Find files by glob pattern. Returns matching file paths relative to the search directory.

- Supports glob patterns like `**/*.go`, `src/**/*.ts`, or `docs/**/*.md`.
- Optional `path` sets the directory to search (default: working directory). `limit` caps the number of results (default: 1000).
- Use it to find files by name or location; use `grep` to search file contents.
- Call multiple searches in parallel when they're independent — speculative parallel searches are often worth it.
