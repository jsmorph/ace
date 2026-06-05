# Development Notes

## Architecture

The repository separates core behavior from external
interfaces. The `core` package owns `Space`, persistence,
matching, identity registration, limits, and provider-backed
dynamic predicates. The `cli`, `netapi`, and `mcp` packages
adapt that core API for command-line use, HTTP, and MCP
stdio, while the root package keeps embedded documentation
and build metadata.

The command binary at `cmd/ace` is a small entry point that
delegates to `cli.Main`. The HTTP API lives in `netapi` so it
can share tests and middleware without depending on command
startup code. The MCP server lives in `mcp` and calls the
same `core.Space` methods as the other interfaces.

The agent-facing skill material lives under `skills/` rather
than `docs/`. Each skill is interface-specific and standalone:
`ace-cli` covers command-line use, `ace-netapi` covers direct
HTTP use, and `ace-mcp` covers remote MCP over Streamable
HTTP. `ace help` prints the CLI skill because the command
line is the context in which help is requested.

The direct HTTP specification excludes MCP. Remote MCP remains
documented in `docs/mcp-spec.md` and
`skills/ace-mcp/SKILL.md`, where examples use complete
JSON-RPC request bodies for Streamable HTTP. The obsolete
original specification was removed because it described
pre-split interfaces and old access-control terms.

## MCP Interface

MCP implementation follows the official Model Context
Protocol revision `2025-06-18`. The transport rules come from
[MCP transports](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports),
which define stdio as newline-delimited JSON-RPC on stdin and
stdout. The tool API comes from
[MCP tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools),
which defines `tools/list`, `tools/call`, tool metadata, and
tool result error handling.

The server implements `initialize`, `notifications/initialized`,
`ping`, `tools/list`, and `tools/call`. Tool results use
`content` plus `structuredContent`, matching the
[MCP schema reference](https://modelcontextprotocol.io/specification/2025-06-18/schema).
ACE operation errors return `isError: true` tool results so a
client can inspect the failure text, while malformed
JSON-RPC requests and unknown tools return protocol errors.

Remote MCP uses Streamable HTTP at `/mcp`. The implementation
returns direct `application/json` responses for requests and
202 for notifications or client responses.
`GET /mcp` returns 405 because ACE does not send unsolicited
server-to-client messages. The endpoint can require bearer
auth through `--mcp-token` or `ACE_MCP_TOKEN`, and it can
restrict browser `Origin` headers through `--mcp-origins`.

Plan:

- [x] Split existing code into `core`, `cli`, and `netapi`.
- [x] Keep the root package for embedded docs and `Commit`.
- [x] Add `mcp` with stdio JSON-RPC framing.
- [x] Expose ACE operations as MCP tools.
- [x] Add tests for initialization, tool listing, and tool
  calls.
- [x] Add Streamable HTTP transport at `/mcp`.
- [x] Add bearer-token and Origin checks for remote MCP.

## SQLite

The pure-Go SQLite driver from
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
registers itself as the `"sqlite"` driver for `database/sql`.

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

Dynamic predicates follow a different path. A key ending in
`~`, `~METRIC<threshold`, or `?` does not generate SQL
branches. Instead, ACE uses the exact branches in the
pattern to narrow the candidate set, then evaluates the
dynamic predicates in Go against each candidate object in
identifier order. This preserves FIFO behavior for `rd` and
`in` without storing embeddings or LLM decisions in SQLite.

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

Dynamic predicates do not compile into Quamina. The
notification path would otherwise need to call a remote API
for every `out`, which would tie commit latency to network
latency and serialize the entire space behind the single
SQLite connection. When a pattern contains embeddings or LLM
predicates, blocking reads fall back to polling.

## Embeddings Matching

ACE parses `field~` as an embeddings predicate with cosine
distance and a default threshold of `0.25`. ACE parses
`field~METRIC<threshold` as an embeddings predicate with an
explicit metric and threshold. The current metrics are
`cosine`, `euclidean`, and `sqeuclidean`. The pattern value
must be a string. The object value at the same path may be a
string or an array of strings, and any matching element
satisfies the predicate.

The OpenAI call uses `POST /v1/embeddings` with model
`text-embedding-3-small` and `encoding_format: "float"`,
following the
[OpenAI embeddings API reference](https://developers.openai.com/api/reference/resources/embeddings/methods/create).
ACE uses `EMBEDDINGS_API_KEY` when it is set, otherwise
`OPENAI_API_KEY`. If neither key is set, ACE returns a
validation error that states that embeddings filtering is
unavailable. `OPENAI_EMBEDDINGS_MODEL` overrides the default
model when a deployment needs a different embeddings model.
`Config.EmbeddingsURL` controls the endpoint URL, and the CLI
exposes it through `--embeddings-url` on `serve`, local
`in`/`rd`, local `match`, and `embcmp`.

## LLM Matching

ACE parses `field?` as an LLM predicate. The pattern value
must be a string. The object value at the same path may be a
string or an array of strings, and any element that yields a
`yes` answer satisfies the predicate.

The current implementation uses `POST /v1/chat/completions`
with a system message that requires `yes` or `no`, plus a
user prompt that includes both `TEXT` and `CTEXT`, following
the
[OpenAI Chat Completions reference](https://platform.openai.com/docs/api-reference/chat/create-chat-completion).
The default model is `gpt-5-mini`, based on the current
[OpenAI models list](https://developers.openai.com/api/docs/models/all/).
ACE uses `LLM_API_KEY` when it is set, otherwise
`OPENAI_API_KEY`. If neither key is set, ACE returns a
validation error that states that LLM filtering is
unavailable. `Config.LLMURL` and `Config.LLMModel` control
the endpoint and model, and the CLI exposes them through
`--llm-url` and `--llm-model` on `serve`, local `in`/`rd`,
and local `match`.

Plan:

- [x] Parse embeddings pattern keys in the pattern parser.
- [x] Call the OpenAI embeddings endpoint lazily from Go.
- [x] Preserve FIFO ordering by scanning exact-match
  candidates in identifier order.
- [x] Keep blocking correct by falling back to polling for
  embeddings patterns.
- [x] Parse `?` pattern keys in the pattern parser.
- [x] Call the LLM endpoint lazily from Go with configurable
  endpoint URL and model.
- [x] Reuse the dynamic candidate-scan path so `?` predicates
  preserve FIFO ordering.
- [x] Keep blocking correct by falling back to polling for
  `?` predicates.

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
informational. The `logSlowOp` method captures
`time.Now()` on entry and checks elapsed time on return via
`defer`.

## Spec Typo

Lines 101-102 of `spec.md` reference `access.out`, but the
API definition (lines 55-58) defines the access fields as
`in` and `rd`. The implementation uses `in` and `rd`.

## Unmatched Properties (`#` prefix)

Properties whose names start with `#` are opaque unmatched data.
They are stored in the object JSON and returned to callers
but excluded from all matching. The filter is applied in
three places: `extractFromObject` (branch.go),
`extractPatternFromObject` (pattern.go), and
`convertToQuamina` (`core/notifier.go`). Each skips `#`-prefixed
keys at every recursion level.

`#` properties in patterns are rejected as errors because
there are no branches to match against. In limits.go,
`walkObject` delegates `#` properties to `walkMeta`, which
checks their values against `ObjectUnmatchableValueSize`
(default 256). Unmatched leaves do not count against the
object leaf limit. `walkPattern` rejects `#` properties.

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

## Identity System

Client identities provide key-based authentication for access
control. The system replaces the honor-system `X-ACE-ID`
header with cryptographic keys that resolve to verified
identities.

### Schema

The `identities` table uses `key` as the primary key because
the hot path is key-to-ID resolution on every authenticated
request. `id` and `name` have UNIQUE constraints, which
provide implicit indexes for lookups and `acen:` resolution
at `out` time.

### Naming scheme

Two prefixes disambiguate IDs from names in access lists:
`ace:` for IDs (generated, 64 hex characters) and `acen:`
for names (user-chosen, 1-20 characters matching
`[a-zA-Z0-9_-]`). When no name is given, the `name` column
stores the `ace:<id>` value.

### Access resolution at out time

`acen:` entries in access lists are resolved to `ace:` IDs at
`out` time, before storage. This means stored access lists
contain only `ace:` IDs, which simplifies the `in`/`rd` query
path: no name resolution at read time. If a name is not
found, `Out` returns a validation error.

### InsecureIDs mode

When `Config.InsecureIDs` is false (the default), access list
entries must carry an `ace:` or `acen:` prefix, and the
`X-ACE-ID` header is rejected. When true, bare strings are
accepted in both contexts for backward compatibility with
existing tests and development use.

### LookupKey atomicity

`LookupKey` updates `last_active` and reads the identity in a
single transaction: `UPDATE` then `SELECT` within
`WithTransaction`. This avoids a separate touch operation and
keeps the last-active timestamp current for identity
expiration. The `RETURNING` clause would be more elegant but
the two-statement approach works with the existing
transaction helper.

### Identity expiration

`DeleteExpiredIdentities` removes rows where `last_active` is
older than `now - IdentityTTL`.  The scavenger runs this with
1-in-10 probability at each scavenge tick, so on average it
executes every 10 intervals.  The default TTL of 40 days
accommodates intermittent clients.

## TLS

`--tls HOSTNAME` enables automatic certificate management via
[autocert](https://pkg.go.dev/golang.org/x/crypto/acme/autocert).
The `autocert.Manager` obtains and renews Let's Encrypt
certificates for the specified hostname.  `HostWhitelist`
restricts issuance to the single configured hostname.

The server binds two listeners: port 443 for HTTPS (using
`tls.Config` from `m.GetCertificate`) and port 80 for HTTP-01
challenge responses and HTTPS redirects (via `m.HTTPHandler`).
Both ports are required by the ACME protocol, so `--addr` is
ignored when `--tls` is set.

Certificates are stored on disk in the `--tls-cache` directory
(default `certs`), using `autocert.DirCache`.  The manager
handles renewal automatically before expiration.

## Throttling

`--throttle RPM` enables per-IP rate limiting (default 60
requests per minute).  The implementation uses a
[token bucket](https://pkg.go.dev/golang.org/x/time/rate)
per IP address.  The rate is `RPM/60` tokens per second with
a burst equal to `RPM`, allowing short spikes up to the full
minute's allowance.  Requests that exceed the limit receive
HTTP 429.

IP addresses are extracted from `r.RemoteAddr` with the port
stripped.  A background goroutine removes entries not seen in
the last five minutes to bound memory.  The map is protected
by a `sync.Mutex`.

## Automatic Updates

The `Updater` in `cli/updater.go` polls a source for new
binaries.  Two source types:

**URL**: HTTP HEAD to `URL/latest/download/ace-GOOS-GOARCH`
on each tick.  Tracks `ETag` and `Last-Modified` headers.  On
change, HTTP GET downloads the binary to a temp file and
`chmod 0755`.

**Directory**: `os.Stat` on the expected binary path.  Tracks
the file's modtime.

Both skip the first check to establish a baseline (the
assumption is that the running binary matches what the source
offers).

The ETag/Last-Modified state is updated only after a
successful download, so a failed download retries on the next
check.  If the server provides neither header, the updater
logs a warning at baseline: change detection requires at least
one.  GitHub releases URLs redirect to a CDN; the standard
HTTP client follows redirects, so the ETag and Last-Modified
come from the CDN.

The update sequence in `cli/main.go`:

1. The `onUpdate` callback (running in the updater goroutine)
   sets the `drainer` flag so new requests get 503, then
   calls `http.Server.Shutdown` with a 10-second timeout.
   `Shutdown` closes the listener, then waits for in-flight
   handlers to return.  Blocking `In`/`Rd` calls use
   `r.Context()`, so `Shutdown` cancels them when the
   deadline expires.  The callback sends the binary path on
   `updateDone`.
2. `srv.Serve` returns `ErrServerClosed` in the main
   goroutine.  The main goroutine checks `drain.draining`:
   if true, it blocks on `updateDone` to ensure `Shutdown`
   is complete, then closes the database explicitly and
   starts the new binary with `os.Args`.

The database is closed before the new process starts.  This
eliminates any window where two processes write to SQLite
concurrently.

The new process calls `listenRetry`, which attempts
`net.Listen` once per second for up to 30 seconds, logging
each attempt.  The port is free after `Shutdown` closes the
listener (step 1), so the retry window covers the startup
delay of the new process.

The `drainer` middleware checks an `atomic.Bool` before
calling the handler.  When draining, new requests receive 503
with a `Retry-After: 30` header.  The drainer is only
installed when `--updates` is set.

## Commit Hash and Ping

`ace.Commit` is a package-level `string` variable set at build
time via `go build -ldflags "-X github.com/morphism/ace.Commit=$(git rev-parse HEAD)"`.
The `/ping` HTTP endpoint and the `ace ping` / `ace version`
CLI subcommands return it as `{"commit":"..."}`.  When the
binary is built without `-ldflags`, the field is empty.

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
