Create and manage a structured checklist for the current session to track progress on a multi-step task. Use it proactively to plan work and to show the user where things stand.

## Exact input shape

```json
{
  "todos": [
    { "content": "Run tests", "active_form": "Running tests", "status": "in_progress" },
    { "content": "Ship it", "active_form": "Shipping it", "status": "pending" }
  ]
}
```

Each call sends the **full** updated list — it replaces the previous one entirely.

| Field | Required | Notes |
| ----- | -------- | ----- |
| `content` | yes | Imperative form of the task, e.g. "Run tests". |
| `active_form` | yes | Present-continuous form shown while the task is running, e.g. "Running tests". |
| `status` | yes | One of `pending`, `in_progress`, `completed`. |

## When to use

- A task needs 3 or more distinct steps, or careful planning.
- The user gave you several things to do, or explicitly asked for a todo list.
- You just started a step (mark it `in_progress`) or finished one (mark it `completed`).

Skip it for a single trivial step or a purely conversational/informational reply — just do the task.

## Rules

- Keep **exactly one** item `in_progress` while work is ongoing (not zero, not two). Calls with more than one `in_progress` item are rejected.
- Update status in real time: mark a task `completed` immediately after finishing it, then move the next one to `in_progress`. Don't batch completions.
- Only mark `completed` when fully done — if it failed, is partial, or is blocked, leave it `in_progress` and add a new item describing what's needed.
- Remove items that are no longer relevant by sending a list without them. Sending an empty list clears the checklist.

## Tool result

Returns a success acknowledgement. On validation failure, returns a structured error naming the offending field with a corrective example.
