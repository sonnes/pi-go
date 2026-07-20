Executes a given bash command with optional timeout. Working directory persists between commands; shell state (everything else) does not. The shell is initialized from the user's profile (bash or zsh).

Use this for terminal operations like git, npm, docker, etc. — not for file reads, edits, writes, or searches (use the dedicated tools).

Before executing:

- If the command creates new directories or files, first `ls` the parent to verify it exists. Example: before `mkdir foo/bar`, run `ls foo`.
- Quote any path containing spaces with double quotes:
  - Correct: `cd "/Users/name/My Documents"`
  - Wrong: `cd /Users/name/My Documents`

Usage notes:

- The `command` argument is required. `timeout` is optional, in seconds — with no timeout applied when omitted.
- Output exceeding 100,000 characters is truncated (the middle is elided).
- Maintain working directory by using absolute paths; avoid `cd` unless the user asks for it. Prefer `pytest /foo/bar/tests` over `cd /foo/bar && pytest tests`.
- Multiple commands: run independent commands as parallel bash calls in one message; chain dependent commands with `&&` in a single call (e.g. `git add . && git commit -m "..." && git push`); use `;` only when you don't care if earlier commands fail. Don't use newlines to separate commands (newlines inside quoted strings are fine).
