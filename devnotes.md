# Development Notes

## Architecture

The `ace` package is both a library and a CLI. The library
(`package ace`) exposes `Space` as the main type with `Out`,
`In`, and `Rd` methods. The CLI (`cmd/ace/main.go`) is a
thin wrapper that dispatches subcommands.

## SQLite

The pure-Go SQLite driver from
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
is the only external dependency. It registers itself as the
`"sqlite"` driver for `database/sql`.

Connection configuration (set via DSN pragmas):

| Pragma | Value | Reason |
|--------|-------|--------|
| `journal_mode` | WAL | Concurrent reads during writes |
| `busy_timeout` | 5000ms | Avoid immediate SQLITE_BUSY |
| `foreign_keys` | ON | Required per-connection for CASCADE |

`SetMaxOpenConns(1)` serializes all database access through a
single connection. SQLite does not support true concurrent
writes, so this avoids contention errors. For v1, both reads
and writes share this connection. If read-heavy workloads
appear, a separate read-only connection pool could be added.

The `ON DELETE CASCADE` on the `access` and `branches`
foreign keys means that
`DELETE FROM objects WHERE id = ?` (used by `in()`)
automatically cleans up related rows.

## Branch Encoding

A branch is a root-to-leaf path in a JSON object, encoded as
`path.to.prop=value`. The encoding must be deterministic so
that SQL equality checks work between object branches and
pattern branches.

Property name escaping: `.` `=` `\` are escaped with `\`
prefix. String values are quoted with `"` and internal `"`
and `\` are escaped. Numbers are normalized via
`strconv.FormatFloat(v, 'f', -1, 64)`, which maps `1.0` and
`1` to the same string `"1"`.

Arrays in pattern leaves mean "match any of these values":
`[1,2]` means "match 1 or 2". A single value is equivalent
to a one-element array, so `{"a":1}` and `{"a":[1]}` produce
identical pattern branches. Arrays in object values generate
one branch per element: `{"a":[1,2,3]}` produces branches
`a=1`, `a=2`, `a=3`. Array elements must be atomic (no
nested objects or arrays). The `ObjectArrayLength` limit
(default 4) caps array size, and each element counts as one
leaf against `ObjectLeaves`.

## Pattern Matching via SQL

Each pattern branch becomes an `EXISTS` subquery against the
`branches` table. For atomic leaves, `b = ?`. For array
leaves, `b IN (?, ?, ...)`. All branches are ANDed. Access
control adds a clause that checks either no access
restriction exists for the operation type, or the caller's ID
appears in the access list.

## Blocking

Two implementations, selected by `Config.Blocking`:

**Polling** (`BlockingPoll`): backoff intervals of 50ms,
100ms, 200ms, then 500ms. Re-executes the SQL query on each
iteration.

**Notification** (`BlockingNotify`, default): uses
[Quamina](https://pkg.go.dev/quamina.net/go/quamina) for
in-memory pattern matching. When `wait > 0`, the client
registers its pattern in a shared Quamina instance, then
waits on a channel. When `Out()` commits a new object,
`MatchesForEvent` runs synchronously (providing
backpressure) and signals all matching waiters. Each waiter
re-executes the SQL query on wake-up to handle access
control, `since`, and `in`-delete atomicity.

The register-then-requery protocol eliminates a race:
register the pattern first, then re-query the database. If an
`Out()` committed between the initial query and registration,
the re-query catches it. If `Out()` commits after
registration, the Quamina signal catches it.

Quamina is not thread-safe, so all access is serialized by
`Notifier.mu`. Channel signals are sent outside the lock to
avoid holding it while goroutines receive.

Ace patterns convert to Quamina format by wrapping atomic
leaves in arrays: `{"a":1}` becomes `{"a":[1]}`. Array
leaves are already in Quamina format.

## Indexes

The hot query path is `BuildMatchQuery`, which scans
`objects` by primary key and runs correlated `EXISTS`
subqueries against `branches` and `access` for each
candidate row.

| Index | Columns | Purpose |
|-------|---------|---------|
| PK | `objects(id)` | ORDER BY id ASC LIMIT 1 |
| `idx_branches` | `branches(id, b)` | Covering for branch EXISTS |
| `idx_access` | `access(id, type, iid)` | Covering for access EXISTS |
| `idx_objects_delete_id` | `objects(delete_id) WHERE NOT NULL` | Partial for `Del` lookups |
| `idx_objects_expires` | `objects(expires)` | For expiration filter |

The `branches` index was originally on `(b)` alone, which
forced SQLite to scan all rows with a matching branch value
across every object. The composite `(id, b)` turns each
correlated EXISTS into a direct two-column lookup. The
`access` index was `(id, type)`; extending it to include
`iid` makes the authorized-caller check a covering-index
lookup. The partial index on `delete_id` excludes rows where
`delete_id IS NULL` (the majority), keeping the index small.

`TestQueryPlans` verifies via `EXPLAIN QUERY PLAN` that all
subqueries use SEARCH with the expected indexes. This
prevents regressions.

## Latency Monitoring

`Config.DBOperationTimeMonitorLimit` (default 1 second)
controls a per-operation timer. When any database operation
(`out`, `in`, `rd`, `del`, `delete expired`, `stats`) exceeds
this threshold, a warning is logged to stderr:

```
WARN high latency 1.234s for out
```

A limit of 0 disables monitoring. High latency is
informational, not an error. The `logSlowOp` method captures
`time.Now()` on entry and checks elapsed time on return via
`defer`.

## Spec Typo

Lines 101-102 of `spec.md` reference `access.out`, but the
API definition (lines 55-58) defines the access fields as
`in` and `rd`. The implementation uses `in` and `rd`.

## Metadata Properties (`#` prefix)

Properties whose names start with `#` are opaque metadata.
They are stored in the object JSON and returned to callers
but excluded from all matching. The filter is applied in
three places: `extractFromObject` (branch.go),
`extractPatternFromObject` (pattern.go), and
`convertToQuamina` (notifier.go). Each skips `#`-prefixed
keys at every recursion level.

`#` properties in patterns are rejected as errors because
there are no branches to match against. `walkObject`
(limits.go) also skips `#` properties so they don't count
against the object leaf limit, while `walkPattern` rejects
them.

Quamina's `MatchesForEvent` receives the full object JSON
(including `#` fields). Since registered patterns never
reference `#` fields, Quamina ignores them. No filtering is
needed on the notification object path.

## Decisions

**Arrays in object values.** An array of atomics in an object
value produces one branch per element. `{"a":[1,2]}`
generates branches `a=1` and `a=2`, so pattern `{"a":1}` and
pattern `{"a":[2,3]}` both match. Quamina handles arrays in
events natively (it matches if any element satisfies the
pattern), so the notification path requires no special
conversion.

**Number normalization.** `float64` representation with
`FormatFloat` normalizes `1` and `1.0` to the same branch
string. This prevents subtle matching failures from JSON
serialization differences.

**Notification as wake-up signal, not data delivery.** The
Quamina notification sends a signal, not the matched object.
The waiter always re-executes the SQL query. This keeps all
correctness logic (access control, `since` filtering,
`in`-delete atomicity) in one place. False wake-ups (access
mismatch, consumed by another `in` waiter) are harmless: one
extra SQL query, then back to sleep.

**Single DB connection.** `SetMaxOpenConns(1)` is the
simplest correct configuration for SQLite. If read contention
becomes measurable, split into writer + reader pool.

## Explicit Deletes

SQS-style visibility timeout for `in` operations. When
`Config.Deletes` is true, `in` does not delete the object
row. Instead it sets `delete_id` (a 32-character hex token
from `crypto/rand`) and `invisible_until` (timestamp = now +
visibility timeout). The query filter
`(invisible_until IS NULL OR invisible_until <= now)` hides
the object during this window.

`Del(deleteID)` permanently deletes the object by executing
`DELETE FROM objects WHERE delete_id = ? AND invisible_until > now`.
The `invisible_until > now` check prevents stale delete_ids
from taking effect after the timeout expires.

The no-overlap invariant holds because: (1) `in` only selects
visible objects (invisible_until IS NULL or in the past), so
an object with an active delete_id is skipped. (2) The
SELECT + UPDATE runs inside a transaction with
`SetMaxOpenConns(1)`, making it atomic. (3) After the timeout
expires, the old delete_id remains but `Del` rejects it due
to the `invisible_until > now` check. A new `in` can then
select the object and assign a fresh delete_id.
