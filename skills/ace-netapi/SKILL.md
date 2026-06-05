# ACE HTTP API

Use this skill when an agent coordinates through the ACE HTTP API. The API exposes the ACE tuple space over HTTP endpoints with JSON request and response bodies. Use it for services, workers, and agents that need shared coordination across processes or machines without running the `ace` CLI.

## Core Model

ACE stores JSON objects and returns the earliest visible object that matches a JSON pattern. Producers write objects with `POST /out`. Consumers claim work with `POST /in` or inspect state with `POST /rd`. Objects have TTLs. Optional access lists restrict which registered identities may read or consume an object.

Use matched fields for selection:

```json
{"type":"task","queue":"review","status":"ready","priority":2}
```

Use `#`-prefixed fields for metadata that should be returned but ignored by matching:

```json
{"type":"task","queue":"review","#trace":"run-42","#notes":"planner output"}
```

Patterns require every specified field to match. Arrays express alternatives:

```json
{"type":"task","queue":["review","repair"],"status":"ready"}
```

## Endpoint Summary

| Method | Path | Use |
|--------|------|-----|
| `POST` | `/out` | Write an object |
| `POST` | `/in` | Claim and consume one matching object |
| `POST` | `/rd` | Read one matching object without consuming it |
| `POST` or `GET` | `/del` | Confirm explicit deletion |
| `POST` or `GET` | `/match` | Test object/pattern matching |
| `GET` | `/stats` | Read storage statistics |
| `GET` | `/limits` | Read active size and TTL limits |
| `POST` | `/reg` | Register an identity |
| `GET` | `/regcheck` | Look up an identity |
| `GET` | `/ping` | Read build metadata |
| `GET` | `/doc` | List embedded documentation |

Remote MCP is a separate interface at `/mcp`. Use the ACE MCP skill when the client uses MCP.

## Writing Objects

Request:

```http
POST /out
Content-Type: application/json
```

```json
{
  "object": {"type":"task","queue":"review","status":"ready"},
  "ttl": "PT2H"
}
```

Response:

```json
{"id":"2026-06-05T14:22:09.123456789"}
```

Restrict access when only specific identities should consume or read the object:

```json
{
  "object": {"type":"task","queue":"private","status":"ready"},
  "access": {
    "in": ["ace:worker-id"],
    "rd": ["ace:monitor-id"]
  }
}
```

Omit `access` for public objects. Empty `in` or `rd` lists deny that operation to everyone.

## Claiming Work

Use `/in` to claim one matching object:

```http
POST /in
Content-Type: application/json
X-ACE-Client-Key: ...
```

```json
{
  "pattern": {"type":"task","queue":"review","status":"ready"},
  "wait": "PT30S"
}
```

Response when a match exists:

```json
{
  "id": "2026-06-05T14:22:09.123456789",
  "object": {"type":"task","queue":"review","status":"ready"}
}
```

Response when no object matches:

```json
null
```

Use bounded waits instead of tight polling. A request blocked by `wait` completes when a matching object appears, the deadline expires, or the client disconnects.

## Reading State

Use `/rd` for observation. It accepts the same request body as `/in`, but it leaves the object in the space:

```json
{"pattern":{"type":"state","component":"worker-7"}}
```

Use `/rd` for shared state, capability records, heartbeats, and event scans. Use `/in` only when the agent will act on the claimed object.

## Explicit Deletes

When the server enables explicit deletes, `/in` returns a `delete_id`:

```json
{
  "id": "2026-06-05T14:22:09.123456789",
  "object": {"type":"task","queue":"review","status":"ready"},
  "delete_id": "a1b2c3..."
}
```

The object becomes invisible for the visibility timeout. Confirm completion:

```http
POST /del
Content-Type: application/json
```

```json
{"delete_id":"a1b2c3..."}
```

If the worker fails before confirmation, the object reappears after the timeout.

## Identity and Access

Create an identity:

```http
POST /reg
Content-Type: application/json
```

```json
{"name":"worker"}
```

The response contains `key`, `id`, and `name`. Send the key on reads and claims:

```http
X-ACE-Client-Key: ...
```

`X-ACE-ID` is only valid when the server allows insecure IDs. Agents should use keys for remote calls.

## HTTP Usage

Keep requests small. ACE validates object, pattern, access, TTL, and identity sizes. Use patterns with exact routing fields such as `type`, `queue`, `status`, `component`, and `capability`. Put large text and trace data under `#` keys unless agents need to match it.

Use `wait` on `/in` for task workers. Use `rd` with `since` for event reads:

```json
{"pattern":{"type":"event","stream":"build"},"since":"2026-06-05T14:22:09.123456789"}
```

Check `/stats` when diagnosing blocked reads, unexpected object counts, or access-list growth. Check `/limits` before generating large objects, large patterns, or long TTLs.

## Dynamic Predicates

Embedding predicates use `~`:

```json
{"type":"note","context~":"TexMex food"}
```

LLM predicates use `?`:

```json
{"type":"note","comment?":"TexMex food"}
```

The server controls model endpoints and keys. Prefer exact fields for routing. Use dynamic predicates when exact fields cannot express the match.

## Errors

Validation errors return 400 with `{"error":"..."}`. Server failures return 500. Too many blocking clients may return 503. Rate limiting may return 429 when the server runs with throttling. Treat `null` as a normal no-match result.
