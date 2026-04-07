# ACE: Agent Coordination Engine

ACE is a coordination service for software agents. This
service is a generalization of a [tuple
space](https://dl.acm.org/doi/10.1145/2363.2433): a shared,
persistent store where agents communicate by writing and
reading JSON objects. Agents coordinate through the objects
themselves, matched by patterns, without knowing about each
other.

ACE is built with Go and SQLite and exposes an HTTP API and
a CLI.

## Background

In 1985, David Gelernter introduced Linda, a coordination
language for parallel programs. The central idea: processes
communicate not by sending messages to each other but by
writing tuples into a shared associative memory and
retrieving them by pattern. Three operations define the
model. `out` places a tuple in the space. `in` retrieves and
removes a matching tuple, blocking if none exists. `rd` reads
without removing. Because a producer does not name a
recipient and a consumer does not name a source, the two are
decoupled in both time and identity. A process can write a
result and terminate; another process, started hours later,
can retrieve it.

Autonomous AI agents face the same coordination problem. An
orchestrator decomposes work into tasks. Worker agents claim
tasks, produce results, and may spawn further tasks. Monitors
observe progress. The number and identity of agents can
change at any time. Message queues and pub/sub systems handle
some of these patterns, but they require the sender to choose
a destination queue or topic. A tuple space lets agents
coordinate through the content of the data: write a JSON
object describing a task, and any agent whose pattern matches
it can retrieve it. No routing configuration, no queue names,
no broker topics.

ACE adapts Gelernter's model for this setting, replacing
tuples with JSON objects and adding access control, TTL, and
an HTTP interface. The [usage guide](docs/guide.md) walks through
three realistic scenarios.

## Operations

ACE provides three operations borrowed from the tuple-space
tradition.

`out(object, access, ttl)` writes an object into the space.

`in(callerID, pattern, wait, since)` finds and removes the
earliest object matching the pattern. If no match exists and
`wait` is greater than zero, the call blocks for up to that
duration. The optional `since` parameter skips objects
with timestamps at or before the given value, enabling
cursor-style iteration.

`rd(callerID, pattern, wait, since)` works like `in` but
does not remove the object.

Each object receives a unique nanosecond-resolution timestamp
as its identifier. Operations return the earliest matching
object first.

ACE also supports `del(delete_id)`, which permanently deletes an
object previously marked invisible by `in` when explicit deletes are
enabled (see Explicit Deletes below).

## Pattern matching

A pattern is a JSON object. Matching decomposes both pattern
and object into branches: root-to-leaf paths through the JSON
structure. `{"a":{"b":1},"c":2}` has branches `a.b=1` and
`c=2`. An object matches when every branch in the pattern
appears in the object.

An array in a pattern means "any of these values":
`{"a":[1,2]}` matches if the object contains `a=1` or `a=2`.
An array in an object produces one branch per element:
`{"a":[1,2,3]}` produces `a=1`, `a=2`, and `a=3`. Extra
fields in the object do not prevent a match. The empty
pattern `{}` matches any object.

A property name ending in `~` enables embeddings matching for
that leaf. The pattern value must be a string, and ACE
compares it with the string value at the same object path
using OpenAI embeddings. `field~` uses cosine distance with a
default threshold of `0.25`, while
`field~METRIC<threshold` selects an explicit distance metric
and threshold. The supported metrics are `cosine`,
`euclidean`, and `sqeuclidean`. ACE uses
`EMBEDDINGS_API_KEY` when it is set, otherwise
`OPENAI_API_KEY`. If neither key is set, ACE returns an
error instead of attempting embeddings matching.

The embeddings client defaults to OpenAI's
`/v1/embeddings` endpoint, but ACE can point to any compatible
endpoint. `ace serve` accepts `--embeddings-url` to configure
the server's embeddings client. Local CLI commands that can
evaluate embeddings patterns, including `ace in`, `ace rd`,
`ace match`, and `ace embcmp`, accept the same flag.

A property name ending in `?` enables LLM-based relation
matching for that leaf. The pattern value must be a string.
ACE sends the candidate text and the pattern text to an LLM
with a fixed prompt that asks whether `TEXT` is related to
`CTEXT`, requiring the model to answer `yes` or `no` and
nothing else. ACE uses `LLM_API_KEY` when it is set,
otherwise `OPENAI_API_KEY`. If neither key is set, ACE
returns an error instead of attempting LLM matching.

The default LLM client uses OpenAI's chat-completions
endpoint with model `gpt-5-mini`, but ACE can point to any
compatible endpoint and model. `ace serve` accepts
`--llm-url` and `--llm-model` to configure the server's LLM
client. Local CLI commands that can evaluate `?` predicates,
including `ace in`, `ace rd`, and `ace match`, accept the
same flags.

Like embeddings predicates, `?` predicates use exact SQL
branches to narrow candidates first, then evaluate the LLM
predicate in object ID order. Blocking reads with `?`
predicates fall back to polling instead of Quamina
notifications.

| Pattern | Object | Match? |
|---------|--------|--------|
| `{"a":1}` | `{"a":1}` | yes |
| `{"a":[1,2]}` | `{"a":1}` | yes |
| `{"a":[1,2]}` | `{"a":3}` | no |
| `{"a":[1,2]}` | `{"a":1,"b":0}` | yes |
| `{"b":[1,2]}` | `{"a":1}` | no |
| `{"b":[1,2]}` | `{"a":3,"b":1}` | yes |
| `{"a":{"b":1,"c":2}}` | `{"a":{"b":1,"c":2,"d":3}}` | yes |
| `{"a":{"b":1,"c":2},"d":3}` | `{"a":{"b":1,"c":2,"d":3}}` | no |

The last row fails because the pattern requires branch `d=3`
at the root, but the object has `d=3` only under `a`.

## Unmatched properties

Properties whose names start with `#` are unmatched: stored in
the object and returned to callers, but excluded from pattern
matching. They do not count against the object leaf limit. A
`#` property in a pattern is an error. See the
[specification](docs/spec.md) for details.

## Usage

Build the binary:

```
make build
```

This embeds the git commit hash in the binary (reported by
`ace ping` and `GET /ping`).

Start a server:

```
ace serve --addr localhost:8000
```

Start a server with an explicit embeddings endpoint:

```
ace serve --addr localhost:8000 \
  --embeddings-url https://example.com/v1/embeddings
```

Start a server with an explicit LLM endpoint and model:

```
ace serve --addr localhost:8000 \
  --llm-url https://example.com/v1/chat/completions \
  --llm-model gpt-5-mini
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

Read with embeddings matching:

```
curl -X POST http://localhost:8000/rd \
  -d '{"pattern":{"type":"task","context~":"TexMex food"}}'
```

Read with LLM relation matching:

```
curl -X POST http://localhost:8000/rd \
  -d '{"pattern":{"type":"task","comment?":"TexMex food"}}'
```

Read and remove:

```
curl -X POST http://localhost:8000/in \
  -d '{"pattern":{"type":"task"}}'
```

Block until a match appears (up to 10 seconds):

```
curl -X POST http://localhost:8000/in \
  -d '{"pattern":{"type":"task"},"wait":"10s"}'
```

The CLI commands operate on a local database by default.
With `--server` (or the `$ACE_URL` environment variable),
they send requests to a remote ACE server instead:

```
ace out --object '{"type":"task","payload":"compute"}'
ace rd --pattern '{"type":"task"}'
ace in --pattern '{"type":"task"}'
ace del --delete-id <id>
ace stats
ace ping
```

`ace match` tests a pattern locally without a database or
server:

```
ace match --object '{"type":"task","priority":1}' \
  --pattern '{"type":"task"}'

ace match \
  --object '{"type":"task","comment":"tacos and queso"}' \
  --pattern '{"type":"task","comment?":"TexMex food"}' \
  --llm-url https://example.com/v1/chat/completions \
  --llm-model gpt-5-mini
```

`ace embcmp` compares two texts with the same embeddings
client and distance code used by embeddings-based pattern
matching. It reports the selected metric and distance, then
also reports cosine similarity, dot product, and both
Euclidean distances for the same pair:

```
ace embcmp --query 'TexMex food' --text 'tacos and queso'
ace embcmp --query 'TexMex food' --text 'tacos and queso' \
  --metric euclidean \
  --threshold 1.0 \
  --embeddings-url https://example.com/v1/embeddings
```

The built-in stress test launches concurrent writers and
readers over HTTP and verifies that every written object is
consumed exactly once:

```
ace test --writers 8 --readers 8 --requests 200
```

## HTTP API

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/out` | Write an object |
| POST | `/in` | Read and remove a matching object |
| POST | `/rd` | Read a matching object |
| POST/GET | `/match` | Test object/pattern match |
| POST/GET | `/del` | Confirm deletion of an object |
| GET | `/limits` | Return active limits |
| GET | `/stats` | Return storage statistics |
| POST | `/reg` | Register a client identity |
| GET | `/regcheck` | Look up a client identity |
| GET | `/ping` | Return the server's commit hash |
| GET | `/doc` | List embedded documentation |
| GET | `/doc/{name}` | Return a documentation file |

See the [HTTP API specification](docs/http-spec.md) for
request/response formats and error codes.

## Access control

An object can restrict who may read or consume it. Pass an
`access` parameter to `out` with lists of permitted caller
identities:

```
curl -X POST http://localhost:8000/out \
  -d '{"object":{"type":"task","payload":"compute"},"access":{"in":["ace:a1b2..."],"rd":["acen:monitor"]}}'
```

Only the identity `ace:a1b2...` can consume this object
(`in`) and only `acen:monitor` can read it (`rd`).

Callers authenticate with the `X-ACE-Client-Key` header,
which the server resolves to an `ace:` identity. Register a
client identity first with `POST /reg` (or `ace reg` on the
CLI):

```
curl -X POST http://localhost:8000/reg -d '{"name":"monitor"}'
```

Then use the returned key to authenticate:

```
curl -X POST http://localhost:8000/in \
  -H "X-ACE-Client-Key: <key>" \
  -d '{"pattern":{"type":"task"}}'
```

Access lists accept both `ace:` IDs and `acen:` names. Names
are resolved to IDs at `out` time. If an object has no
`access` parameter, any caller can read or consume it. See
the [specification](docs/spec.md) for full access control rules
and the `--insecure-ids` mode for development.

## TTL

The optional `ttl` parameter on `out` sets the object's
lifetime as an ISO 8601 duration (e.g., `P3D` for three
days, `PT2H` for two hours). The default is 72 hours.
Expired objects become invisible to `in` and `rd`.

## Explicit deletes

When explicit deletes are enabled (`--deletes`), `in` does
not remove objects immediately. Instead it marks the object
invisible and returns a `delete_id`. The caller confirms
deletion by calling `del` with that ID within the visibility
timeout (default: 30 seconds). If confirmation does not
arrive in time, the object reappears in the space. See the
[specification](docs/spec.md) for details.

## Configuration

The server accepts flags for blocking mode, scavenge interval,
max waiters, limits, TLS, per-IP throttling, and automatic
updates.  The [CLI reference](docs/cli-spec.md) documents all
flags; the [specification](docs/spec.md) lists limits and
their defaults.

## Event pattern matching

AWS EventBridge routes events by matching their JSON content
against patterns that specify required field values. Tim
Bray's [Quamina](https://github.com/timbray/quamina) is an
open-source implementation of that matching engine, optimized
for large numbers of patterns evaluated against each incoming
event. ACE uses Quamina to notify blocked clients: when a new
object enters the space, Quamina identifies which waiting
clients have patterns that match it, and those clients wake
up to execute their queries.

## Documentation

| Document | Scope |
|----------|-------|
| [Specification](docs/spec.md) | Operations, matching, access, TTL, limits |
| [HTTP API](docs/http-spec.md) | Endpoints, request/response formats |
| [CLI reference](docs/cli-spec.md) | Subcommands, flags, stdin behavior |
| [Usage guide](docs/guide.md) | Worked examples for three scenarios |
| [Related systems](docs/related.md) | Real-world analogs to the tuple-space model |
| [Pattern extensions](docs/extensions.md) | Numeric and string inequality matching via Quamina fork |
| [Overview](docs/skill.md) | Summary for agents and `ace help` output |

## Dependencies

[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
is a pure-Go SQLite implementation (no CGo).
[Quamina](https://pkg.go.dev/quamina.net/go/quamina) provides
content-based pattern matching for blocking notifications.
[golang.org/x/crypto/acme/autocert](https://pkg.go.dev/golang.org/x/crypto/acme/autocert)
manages TLS certificates from Let's Encrypt.
[golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)
provides token-bucket rate limiting for per-IP throttling.

## Roadmap

- [ ] Other binary relations in addition to equality.  Examples:
      numeric `<`, `<=`; string versions; prefix.  See what Quamina
      supports as a guide.  We'd need to keep it simple to have SQL
      equivalents.

- [ ] Support updating visibility timeout for an emitted
      object (as in SQS).

- [ ] Server-sent events to deliver multiple objects
      asynchronously for `in` and `rd` operations if
      requested.

- [ ] Maybe? Modify `ace in` to accept `--n N`, which gets
      `N` objects, each of which is written on its own line
      to `stdout`. Similarly for `ace rd`.

- [ ] Probably not: Support unification in pattern matching.

## References

1. David Gelernter, [Generative Communication in
   Linda](https://dl.acm.org/doi/10.1145/2363.2433), ACM
   TOPLAS, 1985. The original tuple-space paper.
1. [Quamina](https://github.com/timbray/quamina):
   Content-based pattern matching for JSON events. Tim
   Bray's open-source implementation of the approach used
   by AWS EventBridge.
