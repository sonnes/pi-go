Reads a file from the local filesystem. Assume any path the user gives is valid; reading a non-existent file returns an error.

Usage:

- Prefer absolute paths. Relative paths resolve against the session working directory; paths escaping it are rejected.
- Reads up to 2000 lines starting at the beginning by default. `offset` and `limit` are available for long files but it's better to read the whole file when you can.
- Lines longer than 2000 characters are truncated.
- Output uses `cat -n` format — line numbers start at 1.
- Reads images (PNG, JPG, etc. — presented visually), PDFs (extracted text, with the original attached), and Jupyter notebooks (cells with outputs).
- Files only — to list a directory, use `ls` via `bash`.
- For screenshots provided as a path, ALWAYS use this tool — it works with temporary file paths.
