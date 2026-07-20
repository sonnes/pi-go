Performs exact string replacements in a file. For a single change pass `old_string`/`new_string` directly; for several disjoint changes pass an `edits` array. Provide one form or the other, not both. Each `old_string` is matched against the original file content and replaced with `new_string`.

Usage:

- Prefer absolute paths. Relative paths resolve against the session working directory.
- Match exact indentation. The `read` tool prefixes each line with `<spaces><line number><tab>`; everything after that tab is the actual content. Never include any line-number prefix in `old_string` or `new_string`.
- ALWAYS prefer editing existing files. Never create new files unless explicitly required.
- Each `old_string` must be unique in the file — the edit FAILS otherwise. Provide more surrounding context to make it unique.
- Pass multiple edits in one call for disjoint changes. Edits must not overlap; if two changes touch the same block or nearby lines, merge them into one edit.
