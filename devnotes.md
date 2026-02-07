# Development Notes

## Architecture

The `ace` package is both a library and a CLI. The library (`package ace`) exposes `Space` as the main type with `Out`, `In`, and `Rd` methods. The CLI (`cmd/ace/main.go`) is a thin wrapper that dispatches subcommands.

## SQLite

The pure-Go SQLite driver from [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) is the only external dependency. It registers itself as the `"sqlite"` driver for `database/sql`.

Connection configuration (set via DSN pragmas):

| Pragma | Value | Reason |
|--------|-------|--------|
| `journal_mode` | WAL | Concurrent reads during writes |
| `busy_timeout` | 5000ms | Avoid immediate SQLITE_BUSY on contention |
| `foreign_keys` | ON | Required per-connection for ON DELETE CASCADE |

`SetMaxOpenConns(1)` serializes all database access through a single connection. SQLite does not support true concurrent writes, so this avoids contention errors. For v1, both reads and writes share this connection. If read-heavy workloads appear, a separate read-only connection pool could be added.

The `ON DELETE CASCADE` on the `access` and `branches` foreign keys means that `DELETE FROM objects WHERE id = ?` (used by `in()`) automatically cleans up related rows.

## Branch Encoding

A branch is a root-to-leaf path in a JSON object, encoded as `path.to.prop=value`. The encoding must be deterministic so that SQL equality checks work between object branches and pattern branches.

Property name escaping: `.` `=` `\` are escaped with `\` prefix. String values are quoted with `"` and internal `"` and `\` are escaped. Numbers are normalized via `strconv.FormatFloat(v, 'f', -1, 64)`, which maps `1.0` and `1` to the same string `"1"`.

Arrays are not valid in object values. They appear only in pattern leaves, where `[1,2]` means "match 1 or 2".

## Pattern Matching via SQL

Each pattern branch becomes an `EXISTS` subquery against the `branches` table. For atomic leaves, `b = ?`. For array leaves, `b IN (?, ?, ...)`. All branches are ANDed. Access control adds a clause that checks either no access restriction exists for the operation type, or the caller's ID appears in the access list.

## Blocking

Two implementations, selected by `Config.Blocking`:

**Polling** (`BlockingPoll`): backoff intervals of 50ms, 100ms, 200ms, then 500ms. Re-executes the SQL query on each iteration.

**Notification** (`BlockingNotify`, default): uses [Quamina](https://pkg.go.dev/quamina.net/go/quamina) for in-memory pattern matching. When `wait > 0`, the client registers its pattern in a shared Quamina instance, then waits on a channel. When `Out()` commits a new object, `MatchesForEvent` runs synchronously (providing backpressure) and signals all matching waiters. Each waiter re-executes the SQL query on wake-up to handle access control, `since`, and `in`-delete atomicity.

The register-then-requery protocol eliminates a race: register the pattern first, then re-query the database. If an `Out()` committed between the initial query and registration, the re-query catches it. If `Out()` commits after registration, the Quamina signal catches it.

Quamina is not thread-safe, so all access is serialized by `Notifier.mu`. Channel signals are sent outside the lock to avoid holding it while goroutines receive.

Ace patterns convert to Quamina format by wrapping atomic leaves in arrays: `{"a":1}` becomes `{"a":[1]}`. Array leaves are already in Quamina format.

## Spec Typo

Lines 101-102 of `spec.md` reference `access.out`, but the API definition (lines 55-58) defines the access fields as `in` and `rd`. The implementation uses `in` and `rd`.

## Decisions

**No arrays in object values.** The spec's branch model only defines matching for atomic leaves. An object with an array value (like `{"a":[1,2]}`) has no well-defined branch encoding. Validation rejects arrays in objects.

**Number normalization.** `float64` representation with `FormatFloat` normalizes `1` and `1.0` to the same branch string. This prevents subtle matching failures from JSON serialization differences.

**Notification as wake-up signal, not data delivery.** The Quamina notification sends a signal, not the matched object. The waiter always re-executes the SQL query. This keeps all correctness logic (access control, `since` filtering, `in`-delete atomicity) in one place. False wake-ups (access mismatch, consumed by another `in` waiter) are harmless: one extra SQL query, then back to sleep.

**Single DB connection.** `SetMaxOpenConns(1)` is the simplest correct configuration for SQLite. If read contention becomes measurable, split into writer + reader pool.
