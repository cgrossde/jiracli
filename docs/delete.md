# jiracli — Delete Commands

Reference for: `delete` (dispatches by ref shape).

All delete operations dry-run by default; pass `--yes` to apply. Also accessible as `rm`.

The write safety model (preview → confirm → apply) is identical to the `add`/`edit` groups. See [Write safety model](writes.md#write-safety-model).

---

## Dispatch

`delete` infers what to delete from the ref shape — no subcommand required:

    jiracli delete <ref> [ref...] [--yes] [--profile <name>]

| Ref form | What is deleted |
|---|---|
| `ACME-123` | Entire issue |
| `ACME-123:comment:NNN` | One comment |
| `ACME-123:attach:NNN` | One attachment |
| `ACME-123:link:NNN` | One issue link |
| `ACME-123 ACME-124 ACME-125` | Multiple issues (bulk delete) |

Multi-key mode only applies when every argument is a bare issue key. Comment, attachment, and link refs always require a single argument.

IDs appear inline in `jiracli show <KEY>` output. Link hints are copy-paste ready:

    → jiracli delete ACME-123:link:5860223

### Flags

| Flag | Description |
|---|---|
| `--yes` | Apply without confirmation |
| `--with-subtasks` | Cascade delete to subtasks (issue delete only) |
| `--profile <name>` | Credential profile |

---

## `delete ACME-123:comment:NNN`

Deletes a single comment. Obtain comment IDs from `jiracli show comments <KEY>`.

### Validation

Fetches the comment via `GET /issue/<KEY>/comment/<id>` before showing the preview. If the comment is not found, a `✗` row blocks apply.

### Preview effect line

    − 1 comment on ACME-123 (id: 9421)

### Success output

    ✓ deleted comment 9421 on ACME-123

### Errors

- Comment not found: `comment 9421 on ACME-123 not found` (from `✗` validation row), exit 1.
- 401: `PAT in keychain for profile "X" was rejected (HTTP 401) — run: jiracli auth reauth`

---

## `delete ACME-123:attach:NNN`

Deletes a single attachment. Obtain attachment IDs from the `(id: ...)` suffix in `jiracli show <KEY>` or `jiracli show attachments <KEY>`.

### Validation

Fetches attachment metadata via `GET /attachment/<id>` before showing the preview. If the attachment is not found, a `✗` row blocks apply.

### Preview effect line

    − attachment screenshot.png from ACME-123 (id: 10042)

### Success output

    ✓ deleted attachment 10042 from ACME-123

### Errors

- Attachment not found: `attachment 10042 on ACME-123 not found` (from `✗` validation row), exit 1.

---

## `delete ACME-123:link:NNN`

Deletes a single issue link. Link IDs appear as `(id: NNN)` on each link line in `jiracli show <KEY>` output, with a copy-paste hint: `→ jiracli delete ACME-123:link:NNN`.

### Validation

Jira has no endpoint to fetch a link by ID, so existence cannot be pre-checked. The preview always shows:

    ⚠ link existence not checked — 404 on apply means already gone

### Preview effect line

    − issue link 5860223 from ACME-123 (id: 5860223)

### Success output

    ✓ deleted issue link 5860223 from ACME-123

---

## Bulk issue delete

Delete multiple issues in one command:

    jiracli delete ACME-123 ACME-124 ACME-125
    jiracli delete ACME-123 ACME-124 ACME-125 --yes

All keys are validated before any delete is executed. If any key is not found or has subtasks (without `--with-subtasks`), the entire operation is blocked:

```
cannot proceed — resolve the following first:
  ACME-124 not found
  ACME-125 has 2 subtask(s) — pass --with-subtasks to cascade
[exit:1 | Xms]
```

Dry-run preview:

```
delete 3 issue(s):

  − ACME-123  "Fix login page timeout"
  − ACME-124  "Add dark mode toggle"
  − ACME-125  "Upgrade TLS stack"  ⚠ +2 subtask(s)

Apply with: re-run with --yes
```

On `--yes`: deletes sequentially. Per-key errors (e.g. a concurrent delete) are collected and reported after all others succeed — the batch does not abort on first failure.

### Success output

    ✓ deleted ACME-123
    ✓ deleted ACME-124
    ✓ deleted ACME-125

---

## `delete ACME-123`

Deletes an entire issue. When the issue has subtasks, the command blocks with a `✗` validation error unless `--with-subtasks` is passed.

    jiracli delete ACME-123 [--with-subtasks] [--yes] [--profile <name>]

### Validation

Fetches `GET /issue/<KEY>?fields=key,summary,subtasks` before showing the preview.

| Condition | Row |
|---|---|
| Issue not found | `✗ <KEY> not found` → blocks apply |
| Issue has N subtasks, `--with-subtasks` absent | `✗ <KEY> has N subtask(s) — pass --with-subtasks to cascade` → blocks apply |
| Issue has N subtasks, `--with-subtasks` set | `⚠ will also delete N subtask(s)` |

### Preview effect line

Without subtasks:

    − issue ACME-123

With `--with-subtasks` and N subtasks:

    − issue ACME-123 (and 3 subtask(s))

### Success output

    ✓ deleted issue ACME-123

### Errors

- Issue has subtasks and `--with-subtasks` not set: `ACME-123 has 3 subtask(s) — pass --with-subtasks to cascade`, exit 1.
- Issue not found: `ACME-123 not found`, exit 1.
- Apply fails: `apply failed: <server error>`, exit 1.
