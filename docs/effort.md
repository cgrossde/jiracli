# jiracli — `effort`

Top-level command. Aggregates time estimates and Story Points, then shows how much of the planned time has been spent. (Formerly `show rollup`.)

Hierarchy rollup and result-set aggregation are genuinely different operations, so they live on separate subcommands rather than mutually-exclusive flags:

**Hierarchy mode** — `effort <KEY>`:

    jiracli effort <KEY> [flags]

Walks the direct children of the issue at `<KEY>`. Requires hierarchy fields to be configured (`jiracli setup --reconfigure` or `jiracli config hierarchy --rediscover`).

**JQL mode** — `effort jql <query>`:

    jiracli effort jql '<JQL>' [--group-by assignee|status|statusCategory] [flags]

Aggregates over the result set of an arbitrary JQL query. The query is joined from the positional arguments (quote it when it contains spaces or shell metacharacters). No hierarchy configuration needed.

**Sprint mode** — `effort sprint <id>`:

    jiracli effort sprint <id> [--group-by assignee|status|statusCategory] [flags]

Aggregates over every issue in the sprint with the given numeric id. No hierarchy configuration needed.

By default `effort` reports totals only. Pass `--list` to also see each individual child's own figures, or use [`jiracli hierarchy <KEY>`](hierarchy.md) for the structural (status/assignee/summary, no time figures) tree.

---

## Flags

| Flag | Mode | Default | Description |
|---|---|---|---|
| `--depth N` | `<KEY>` | 1 | Depth to aggregate. 1 = direct children only; 2 = children + grandchildren. Capped at 2. |
| `--group-by <dim>` | all | — | Group rows by dimension. `assignee` is available in `jql`/`sprint` only; `status`/`statusCategory` work everywhere. In hierarchy mode, emits one labeled table per fetched level. |
| `--list` | all | false | Also print a per-child table (key, status, assignee, planned, remaining, spent, SP) beneath the aggregate rows — use this to see how much was logged on each individual child (e.g. each Epic under an Initiative). In hierarchy mode, lists direct (L1) children only, even at `--depth 2`. Combines with `--group-by` (one list per level). |
| `--exclude-done` | all | false | Skip issues in the Done status category |
| `--open` | all | false | Count only non-Done issues (alias for `--exclude-done`) |
| `--state <cat>` | all | — | Count only issues in this status category: `todo`, `in-progress`, `done`, `all`. Takes precedence over `--exclude-done`/`--open`; `all` disables filtering |
| `--since <date>` | all | — | Only count issues updated on or after this date (`-2w`, `-1d`, `2024-01-01`) |
| `--limit N` | all | 100 | Max issues to fetch per level. Increase to see more; use `--all` to fetch everything. |
| `--all` | all | false | Fetch all issues, bypassing the `--limit` cap |
| `--json` | all | false | Output as a single JSON object |
| `--profile <name>` | all | default | Credential profile |

The `--exclude-done` / `--open` / `--state` filter vocabulary is shared with `jiracli search` and `jiracli hierarchy`. Filtering is applied client-side to the fetched issues.

**Truncation is an error, not a silent partial.** Because effort reports aggregated totals, a partial fetch would produce misleading numbers. When more issues match than the `--limit` cap fetched (and `--all` was not passed), the command aborts non-zero rather than aggregating a truncated set:

    effort aggregation incomplete: 1917 issues matched but only 100 were fetched — partial totals would be misleading. Re-run with --all to aggregate every issue, or raise the cap with --limit 1917

This applies to every mode (`<KEY>` hierarchy levels, `jql`, and `sprint`).

---

## Plain-text output shape

```
[Epic]  ACME-100  In Progress · 2 - High
Fix login page timeout

                                         Planned   Remaining       Spent          SP
──────────────────────────────────────────────────────────────────────────────────────
Epic ACME-100 (own)                          30d        7d2h          30d           —
Level 1 — 8 Storys                           12d         10d           2d    22 (5/8)
──────────────────────────────────────────────────────────────────────────────────────
Total                                         42d       17d2h          32d          22

[██████████████████░░░░░░] · 76% spent

  → pass --depth 2 to also aggregate grandchildren
  → jiracli hierarchy ACME-100   # per-child breakdown

[exit:0 | Xms]
```

With `--depth 2` on an Initiative:

```
[Initiative]  PROJ-50  In Progress · —
Modernise authentication platform

                                         Planned   Remaining       Spent          SP
──────────────────────────────────────────────────────────────────────────────────────
Initiative PROJ-50 (own)                       —           —           —           —
Level 1 — 2 Epics                              —           —           —           —
Level 2 — 14 Tasks                          192h        192h           —  19 (12/14)
──────────────────────────────────────────────────────────────────────────────────────
Total (all levels)                          192h        192h           —          19

[░░░░░░░░░░░░░░░░░░░░░░░░] · 0% spent

  (depth 2 is the maximum — run jiracli effort on individual children to go deeper)
  → jiracli hierarchy PROJ-50   # per-child breakdown

[exit:0 | Xms]
```

SP cell: `22 (5/8)` when only 5 of 8 children have Story Points set. Plain `22` when all are pointed. `—` when none.

Progress bar color: white ≤99% spent, orange 100–119%, red ≥120%.

## Per-child breakdown

By default `effort` reports level totals only. Two ways to see individual children:

- **`--list`** — prints a `Children:` table right under the aggregate rows, with each child's own Planned/Remaining/Spent/SP. This is the fastest way to see exactly how much time was logged on each child (e.g. each Epic under an Initiative).
- **`jiracli hierarchy <KEY>`** — one call returns the structural tree with status, assignee, and summary per child (no time-tracking figures). The `effort` footer always links to it.

```
jiracli effort INIT-2702 --list
```

```
[Initiative]  INIT-2702  Open · —
Work Location (Go.Gov)

                                         Planned   Remaining       Spent          SP
──────────────────────────────────────────────────────────────────────────────────────
Initiative INIT-2702 (own)                     —           —           —           —
Level 1 — 14 Epics                          220d       75d2h   125d5h36m           —
──────────────────────────────────────────────────────────────────────────────────────
Total                                       220d       75d2h   125d5h36m           —

[██████████████░░░░░░░░░░] · 57% spent

Children:
Key           Status          Assignee                Planned   Remaining       Spent          SP
────────────────────────────────────────────────────────────────────────────────────────────────
ACME-101      In Progress     Jane Smith                  30d          7d         23d           —
ACME-102      Open            John Doe                    20d         20d           —           —
ACME-103      Done            Jane Smith                  15d           —         15d           —
…

  → pass --depth 2 to also aggregate grandchildren
  → jiracli hierarchy INIT-2702   # per-child breakdown

[exit:0 | Xms]
```

Children are sorted by key. `--list` works in hierarchy mode (lists the L1 children fetched for the aggregate), in `--group-by` mode (one `Children:` table per level), and in `effort jql`/`effort sprint` mode (lists the matched issues).

## `--group-by status` / `--group-by statusCategory` — status breakdown

In **hierarchy mode**, replaces the per-level aggregate rows with a status-grouped table. One labeled table is emitted per fetched level — `--depth 2` yields two tables.

Rows are sorted canonically: blocked → open → in-progress → done. `statusCategory` uses the three universal categories (To Do, In Progress, Done).

```
[Epic]  ACME-100  In Progress · High

Level 1 — 6 Stories
Status                            Count     Planned   Remaining       Spent          SP
─────────────────────────────────────────────────────────────────────────────────────────
Open                                  2         10d         10d           —           5
In Progress                           3         20d          8d         12d          13
Closed                                1         10d           —         10d           5
─────────────────────────────────────────────────────────────────────────────────────────
Total                                 6         40d         18d         22d          23

[████████████░░░░░░░░░░░░] · 55% spent

[exit:0 | Xms]
```

With `--depth 2`:

```
[Initiative]  PROJ-50  Open · —
Modernise authentication platform

Level 1 — 3 Epics
Status                            Count     Planned   Remaining       Spent          SP
─────────────────────────────────────────────────────────────────────────────────────────
In Progress                           2         20d          8d         12d           —
Closed                                1         10d           —         10d           —
─────────────────────────────────────────────────────────────────────────────────────────
Total                                 3         30d          8d         22d           —

[█████████████████░░░░░░░] · 73% spent

Level 2 — 18 Stories
Status                            Count     Planned   Remaining       Spent          SP
─────────────────────────────────────────────────────────────────────────────────────────
Open                                  5         40d         40d           —          15
In Progress                           8         64d         20d         44d          32
Closed                                5         40d           —         40d          20
─────────────────────────────────────────────────────────────────────────────────────────
Total                                18        144d         60d         84d          67

[████████████████░░░░░░░░] · 58% spent

[exit:0 | Xms]
```

In **JQL/sprint mode**, the column header changes to `Status` or `Status Category`, and a `Count` column is added:

```
Rollup: issueType = Epic AND fixVersion = "v2026-Q2"  (31 issues)

Status                                  Count     Planned   Remaining       Spent          SP
───────────────────────────────────────────────────────────────────────────────────────────────
Open                                        8        180d        180d           —           —
In Progress                                15        640d        240d        360d         120
Closed                                      8        320d           —        310d          85
───────────────────────────────────────────────────────────────────────────────────────────────
Total                                      31       1140d        420d        670d         205

→ jiracli show <KEY>  # to drill into any issue
```

With `--group-by statusCategory`:

```
Rollup: sprint = 2001  (31 issues)

Status Category                         Count     Planned   Remaining       Spent          SP
───────────────────────────────────────────────────────────────────────────────────────────────
To Do                                       8        180d        180d           —           —
In Progress                                15        640d        240d        360d         120
Done                                        8        320d           —        310d          85
───────────────────────────────────────────────────────────────────────────────────────────────
Total                                      31       1140d        420d        670d         205

→ jiracli show <KEY>  # to drill into any issue
```

## JSON output shape (`--json`)

Single JSON object:

```json
{
  "subjectKey": "ACME-100",
  "subjectIssueType": "Epic",
  "subjectSummary": "Fix login page timeout",
  "subject": {
    "label": "Epic ACME-100 (own)",
    "originalEstimateSeconds": 864000,
    "remainingEstimateSeconds": 208800,
    "timeSpentSeconds": 864000,
    "storyPoints": 0,
    "pointedCount": 0,
    "totalCount": 1
  },
  "rows": [
    {
      "label": "Level 1 — 8 Storys",
      "originalEstimateSeconds": 345600,
      "remainingEstimateSeconds": 288000,
      "timeSpentSeconds": 57600,
      "storyPoints": 22,
      "pointedCount": 5,
      "totalCount": 8,
      "issueTypeCounts": { "Story": 8 }
    }
  ],
  "nodes": null,
  "hasDeeperLevel": true,
  "maxFetchedDepth": 1,
  "groupBy": "status"
}
```

`nodes` is `null` unless `--list` is passed, in which case it holds one `RollupNode` per child (same shape as `jiracli hierarchy`'s node objects, plus the time-tracking and Story Points fields: `originalEstimateSeconds`, `remainingEstimateSeconds`, `timeSpentSeconds`, `storyPoints`). In hierarchy mode `--list` populates only the L1 children, even at `--depth 2`. `rows` has one entry at `--depth 1`, two at `--depth 2`. `issueTypeCounts` maps issue type name → count within that level; omitted when empty. `hasDeeperLevel` is `true` when any L1 child has its own children. `groupBy` is `"assignee"`, `"status"`, or `"statusCategory"` when `--group-by` was used; omitted otherwise. In hierarchy `--group-by` mode, one JSON object per level is emitted as NDJSON instead of a single object — `nodes` (when `--list` is set) holds that level's own children.

## JQL / sprint mode output

In `effort jql` / `effort sprint` mode, the subject-header block is replaced with a one-line title. The column header changes to match the grouping dimension: `Assignee / Group` (no `--group-by` or `--group-by assignee`), `Status` (`--group-by status`), or `Status Category` (`--group-by statusCategory`). With `--group-by status` or `--group-by statusCategory`, a `Count` column is also added.

Without `--group-by`:

```
Rollup: sprint = 2001  (31 issues)

Assignee / Group                         Planned   Remaining       Spent          SP
──────────────────────────────────────────────────────────────────────────────────────
Total — 31 issues                          640h        240h        380h         120

→ jiracli show <KEY>  # to drill into any issue
```

With `--group-by assignee`:

```
Rollup: sprint = 2001  (31 issues)

Assignee / Group                         Planned   Remaining       Spent          SP
──────────────────────────────────────────────────────────────────────────────────────
Smith, Jane                                 96h         48h         52h          21
Doe, John                                   80h         32h         44h          13
Unassigned                                  16h         16h          —            —
──────────────────────────────────────────────────────────────────────────────────────
Total                                      640h        240h        380h         120

→ jiracli show <KEY>  # to drill into any issue
```

Rows are sorted by `Planned` desc, then `Spent` desc, then name asc. The final `Total` row sums all groups. `Unassigned` groups issues with no assignee.

**JSON note:** in JQL/sprint mode, `subjectIssueType` is `""` and `subject` rows are zeroed; the `rows` array carries the group/total rows. This is distinct from hierarchy mode where `subjectIssueType` is always non-empty.

## Errors

- Hierarchy not configured: `hierarchy fields not configured for profile "X" — run: jiracli config hierarchy --rediscover`, exit 1.
- No children: `KEY has no children — nothing to roll up.`, exit 0.
- Invalid ref: `effort requires a plain issue key — got "<input>"`, exit 1.
