# ACE: Agent Coordination Engine

ACE is a coordination service for software agents. This service is a generalization of a [tuple space](https://dl.acm.org/doi/10.1145/2363.2433): a shared, persistent store where agents communicate by writing and reading JSON objects. Agents coordinate through the objects themselves, matched by patterns, without knowing about each other. 

ACE is built with Go and SQLite and exposes an HTTP API and a CLI.

## Background

In 1985, David Gelernter introduced Linda, a coordination language for parallel programs. The central idea: processes communicate not by sending messages to each other but by writing tuples into a shared associative memory and retrieving them by pattern. Three operations define the model. `out` places a tuple in the space. `in` retrieves and removes a matching tuple, blocking if none exists. `rd` reads without removing. Because a producer does not name a recipient and a consumer does not name a source, the two are decoupled in both time and identity. A process can write a result and terminate; another process, started hours later, can retrieve it.

Autonomous AI agents face the same coordination problem. An orchestrator decomposes work into tasks. Worker agents claim tasks, produce results, and may spawn further tasks. Monitors observe progress. The number and identity of agents can change at any time. Message queues and pub/sub systems handle some of these patterns, but they require the sender to choose a destination queue or topic. A tuple space lets agents coordinate through the content of the data: write a JSON object describing a task, and any agent whose pattern matches it can retrieve it. No routing configuration, no queue names, no broker topics.

ACE adapts Gelernter's model for this setting, replacing tuples with JSON objects and adding access control, TTL, and an HTTP interface. The [usage guide](guide.md) walks through three realistic scenarios.

## Operations

ACE provides four operations borrowed from the tuple-space tradition.

`out(object, access, ttl)` writes an object into the space.

`in(pattern, wait, since)` finds and removes the earliest object matching the pattern. If no match exists and `wait` is greater than zero, the call blocks for up to that many seconds. The optional `since` parameter skips objects with timestamps at or before the given value, enabling cursor-style iteration.

`rd(pattern, wait, since)` works like `in` but does not remove the object.

`del(delete_id)` permanently deletes an object previously marked invisible by `in` when explicit deletes are enabled (see Explicit Deletes below).

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

## Metadata properties

Properties whose names start with `#` are metadata: stored in the object and returned to callers, but excluded from pattern matching. They do not count against the object leaf limit. A `#` property in a pattern is an error. See the [specification](spec.md) for details.

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

Test whether an object matches a pattern (no database required):

```
ace match --object '{"type":"task","priority":1}' --pattern '{"type":"task"}'
```

The remaining CLI commands operate directly on the database without the HTTP server:

```
ace out --object '{"type":"task","payload":"compute"}'
ace rd --pattern '{"type":"task"}'
ace in --pattern '{"type":"task"}'
ace del --delete-id <id>
ace stats
```

The built-in stress test launches concurrent writers and readers over HTTP and verifies that every written object is consumed exactly once:

```
ace test --writers 8 --readers 8 --requests 200
```

## HTTP API

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/out` | Write an object |
| POST | `/in` | Read and remove a matching object |
| POST | `/rd` | Read a matching object |
| POST/GET | `/match` | Test whether an object matches a pattern |
| POST/GET | `/del` | Confirm deletion of an object |
| GET | `/limits` | Return active limits |
| GET | `/stats` | Return storage statistics |

See the [HTTP API specification](http-spec.md) for request/response formats and error codes.

## Access control

An object can restrict who may read or consume it. Pass an `access` parameter to `out` with lists of permitted caller identities:

```
curl -X POST http://localhost:8000/out \
  -d '{"object":{"type":"task","payload":"compute"},"access":{"in":["worker-1"],"rd":["monitor"]}}'
```

Only `worker-1` can consume this object (`in`) and only `monitor` can read it (`rd`). Callers identify themselves with the `X-ACE-ID` header (or `--id` on the CLI):

```
curl -X POST http://localhost:8000/in \
  -H "X-ACE-ID: worker-1" \
  -d '{"pattern":{"type":"task"}}'
```

If an object has no `access` parameter, any caller can read or consume it regardless of whether `X-ACE-ID` is present. If an object has an `access.in` list, only callers whose `X-ACE-ID` appears in that list can consume it. The `access.rd` list works the same way for `rd`.

## TTL

The optional `ttl` parameter on `out` sets the object's lifetime as an ISO 8601 duration (e.g., `P3D` for three days, `PT2H` for two hours). The default is 72 hours. Expired objects become invisible to `in` and `rd`.

## Explicit deletes

When explicit deletes are enabled (`--deletes`), `in` does not remove objects immediately. Instead it marks the object invisible and returns a `delete_id`. The caller confirms deletion by calling `del` with that ID within the visibility timeout (default: 30 seconds). If confirmation does not arrive in time, the object reappears in the space. See the [specification](spec.md) for details.

## Configuration

The server accepts `--blocking` (`polling` or `notify`), `--scavenge` (expiration interval), `--max-waiters` (concurrent blocking client limit), and `--limits` (JSON file overriding default limits). The [CLI reference](cli-spec.md) documents all flags; the [specification](spec.md) lists limits and their defaults.

## Event pattern matching

AWS EventBridge routes events by matching their JSON content against patterns that specify required field values. Tim Bray's [Quamina](https://github.com/timbray/quamina) is an open-source implementation of that matching engine, optimized for large numbers of patterns evaluated against each incoming event. ACE uses Quamina to notify blocked clients: when a new object enters the space, Quamina identifies which waiting clients have patterns that match it, and those clients wake up to execute their queries.

## Specifications

Three specification files document the details:

| Document | Scope |
|----------|-------|
| [Specification](spec.md) | Core operations, pattern matching, access control, TTL, blocking, limits |
| [HTTP API](http-spec.md) | Endpoints, request/response formats, error codes |
| [CLI reference](cli-spec.md) | Subcommands, flags, stdin behavior |
| [Usage guide](guide.md) | Worked examples: software development, research, business workflows |

## Dependencies

ACE has two external dependencies. [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) is a pure-Go SQLite implementation (no CGo). [Quamina](https://pkg.go.dev/quamina.net/go/quamina) provides content-based pattern matching for blocking notifications.

## Roadmap

- [ ] Support updating visibility timeout for an emitted object (as in
      SQS).
 
- [ ] Server-sent events to deliver multiple objects asynchronously
      for `in` and `rd` operations if requested.

- [ ] Maybe? Modify `ace in` to accept `--n N`, which gets `N`
      objects, each of which is written on its own line to `stdout`.
      Similarly for `ace rd`.

- [ ] Probably not: Support unification in pattern matching.


## References

1. David Gelernter, [Generative Communication in Linda](https://dl.acm.org/doi/10.1145/2363.2433), ACM TOPLAS, 1985. The original tuple-space paper.
1. [Quamina](https://github.com/timbray/quamina): Content-based pattern matching for JSON events. Tim Bray's open-source implementation of the approach used by AWS EventBridge.
