Writes a file to the local filesystem, creating it or overwriting what's already there.

Usage:

- Prefer absolute paths. Relative paths resolve against the session working directory.
- Creates the file if it doesn't exist, overwrites it if it does. Parent directories are created automatically.
- Prefer editing an existing file over writing a near-duplicate. For a small change to a file that already exists, use `edit`.
- Keep scratch reasoning in chat; write a file only when the content should outlive the turn.
- Only write emojis to a file when the user asks for them.
