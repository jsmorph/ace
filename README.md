# ACE: Agent Coordination Engine

ACE is a coordination service for software agents. It implements a tuple space: a shared, persistent store where agents communicate by writing and reading JSON objects. Agents coordinate through the objects themselves, matched by pattern, without knowing about each other. The implementation uses Go, SQLite, and an HTTP API.

## Operations

ACE provides three operations borrowed from the tuple-space tradition.

`out(object, access, ttl)` writes an object into the space. The optional `access` and `ttl` parameters are described below.

`in(pattern, wait, since)` finds and removes the earliest object matching the pattern. If no match exists and `wait` is greater than zero, the call blocks for up to that many seconds. The optional `since` parameter skips objects with timestamps at or before the given value, enabling cursor-style iteration.

`rd(pattern, wait, since)` works like `in` but does not remove the object.

Each object receives a unique nanosecond-resolution timestamp as its identifier. Operations return the earliest matching object first.

## Pattern matching

A pattern is a JSON object. An object matches a pattern according to the object's branches: root-to-leaf paths through the JSON structure. The rule:

```
For every branch B with leaf L in the pattern:
  If L is atomic: B appears in the object.
  If L is an array [X1,...,Xn]: B with leaf Xi appears in the object for some i.
```

An array in a pattern means "any of these values." Extra fields in the object do not prevent a match. The empty pattern `{}` matches any object.

| Pattern                     | Object                      | Match? |
|-----------------------------|-----------------------------|--------|
| `{"a":1}`                   | `{"a":1}`                   | yes    |
| `{"a":[1,2]}`               | `{"a":1}`                   | yes    |
| `{"a":[1,2]}`               | `{"a":3}`                   | no     |
| `{"a":[1,2]}`               | `{"a":1,"b":0}`             | yes    |
| `{"b":[1,2]}`               | `{"a":1}`                   | no     |
| `{"b":[1,2]}`               | `{"a":3,"b":1}`             | yes    |
| `{"a":{"b":1,"c":2}}`       | `{"a":{"b":1,"c":2,"d":3}}` | yes    |
| `{"a":{"b":1,"c":2},"d":3}` | `{"a":{"b":1,"c":2,"d":3}}` | no     |

The last row fails because `"d":3` must appear at the top level of the object, but it appears only inside `"a"`.

## Usage

Build the binary:

```
go build -o ace ./cmd/ace
```

Start a server:

```
ace serve --port localhost:8000
```

Write an object:

```
curl -X POST http://localhost:8000/out \
  -d '{"object":{"type":"task","payload":"compute"}}'
```

Response:

```json
{"id":"2025-07-14T22:31:05.123456789"}
```

Read without removing:

```
curl -X POST http://localhost:8000/rd \
  -d '{"pattern":{"type":"task"}}'
```

Read and remove:

```
curl -X POST http://localhost:8000/in \
  -d '{"pattern":{"type":"task"}}'
```

Block until a match appears (up to 10 seconds):

```
curl -X POST http://localhost:8000/in \
  -d '{"pattern":{"type":"task"},"wait":10}'
```

The CLI operates directly on the database without the HTTP server:

```
ace out --object '{"type":"task","payload":"compute"}'
ace rd --pattern '{"type":"task"}'
ace in --pattern '{"type":"task"}'
ace stats
```

Run the built-in stress test, which launches concurrent writers and readers over HTTP and verifies that readers consume every written object exactly once:

```
ace test --writers 8 --readers 8 --requests 200
```

## HTTP API

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/out` | Write an object |
| POST | `/in` | Read and remove a matching object |
| POST | `/rd` | Read a matching object |
| GET | `/limits` | Return current service limits |
| GET | `/stats` | Return database statistics |

The `/in` and `/rd` request body accepts `pattern` (required), `wait` (seconds, default 0), and `since` (timestamp, optional). When no object matches, the response body is `null` with status 200.

## Access control

An object can restrict who may read or consume it. Pass an `access` parameter to `out` with lists of permitted caller identities:

```
curl -X POST http://localhost:8000/out \
  -d '{"object":{"type":"task","payload":"compute"},"access":{"in":["worker-1"],"rd":["monitor"]}}'
```

This object can only be consumed (`in`) by `worker-1` and only be read (`rd`) by `monitor`. Callers identify themselves with the `X-ACE-ID` header (or `--id` on the CLI):

```
curl -X POST http://localhost:8000/in \
  -H "X-ACE-ID: worker-1" \
  -d '{"pattern":{"type":"task"}}'
```

If an object has no `access` parameter, any caller can read or consume it regardless of whether `X-ACE-ID` is present. If an object has an `access.in` list, only callers whose `X-ACE-ID` appears in that list can consume it. The `access.rd` list works the same way for `rd`.

The optional `ttl` parameter on `out` sets the object's lifetime as an ISO 8601 duration (e.g., `P3D` for three days, `PT2H` for two hours). The default is 72 hours.

## Configuration

The `serve` command accepts these flags:

| Flag | Default | Purpose |
|------|---------|---------|
| `--port` | `localhost:8000` | Listen address |
| `--db` | `ace.db` | SQLite database file |
| `--limits` | (none) | JSON file overriding default limits |
| `--blocking` | `notify` | Blocking implementation: `polling` or `notify` |
| `--scavenge` | `PT1H` | Interval for deleting expired objects (ISO 8601) |
| `--max-waiters` | 0 | Max concurrent blocking clients; 0 means unlimited |

When the number of blocked clients reaches `--max-waiters`, new blocking requests receive HTTP 503.

### Limits

These limits apply to every operation. Override them by passing a JSON file to `--limits`.

| Property | Default |
|----------|---------|
| Object size | 2048 bytes |
| Property name size | 64 bytes |
| Object value size | 128 bytes |
| Object leaves | 8 |
| Pattern size | 2048 bytes |
| Pattern leaves | 4 |
| Pattern array length | 4 |
| Pattern atomic leaf size | 128 bytes |
| Access size | 1024 bytes |
| Access identifiers | 16 |
| TTL maximum | 7 days |
| Caller ID size | 128 bytes |

## Background

### Tuple spaces

David Gelernter introduced tuple spaces in 1985 with Linda, a coordination language for parallel programs. The core idea: processes communicate not by sending messages to each other but by writing tuples into a shared associative memory and retrieving them by pattern. The three operations (`out`, `in`, `rd`) decouple producers from consumers in both time and space. ACE applies this model to agent coordination, replacing tuples with JSON objects and adding access control and TTL.

### Event pattern matching

AWS EventBridge routes events by matching their JSON content against patterns that specify required field values. Tim Bray's [Quamina](https://github.com/timbray/quamina) is an open-source implementation of that pattern-matching engine, optimized for large numbers of patterns evaluated against each incoming event. ACE uses Quamina to notify blocked clients: when a new object enters the space, Quamina identifies which waiting clients have patterns that match it, and those clients wake up to execute their queries. The notification approach avoids polling in the common case.

### Norvig's pattern matcher

Peter Norvig's `patmatch` (from *Paradigms of Artificial Intelligence Programming*, 1992) showed that a small pattern language with a few composable operations (literal match, variables, segment variables) expresses non-trivial matching concisely. ACE's pattern language is far simpler: no unification, no variables, no backtracking. But it follows the same principle that matching against structure is more natural and more composable than matching against serialized strings.

## Dependencies

ACE has two external dependencies. [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) is a pure-Go SQLite implementation (no CGo). [Quamina](https://pkg.go.dev/quamina.net/go/quamina) provides content-based pattern matching for blocking notifications.
