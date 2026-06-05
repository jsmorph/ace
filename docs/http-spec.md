# ACE HTTP API

This document specifies the HTTP interface to ACE. See
`spec.md` for the core operations, pattern matching, access
control, TTL, blocking, and limits.

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/out` | Write an object |
| POST | `/in` | Read and remove a matching object |
| POST | `/rd` | Read a matching object |
| POST/GET | `/match` | Test object/pattern match |
| POST/GET | `/del` | Confirm deletion of an object |
| GET | `/limits` | Return the active limits |
| GET | `/stats` | Return storage statistics |
| POST | `/reg` | Register a client identity |
| GET | `/regcheck` | Look up a client identity |
| GET | `/ping` | Return the server's commit hash |
| POST/GET | `/mcp` | MCP Streamable HTTP endpoint |
| GET | `/doc` | List embedded documentation |
| GET | `/doc/{name}` | Return a documentation file |

All request and response bodies use JSON with content type
`application/json`.

## POST /out

Write an object into the space.

Request body:

```json
{
  "object": {"#id": "job-42", "type": "task", "payload": "compute"},
  "access": {"in": ["worker-1"], "rd": ["monitor"]},
  "ttl": "P1D"
}
```

| Field | Required | Purpose |
|-------|----------|---------|
| `object` | yes | JSON object to store (`#` properties are unmatched; see `spec.md`) |
| `access` | no | Access control (see `spec.md`) |
| `ttl` | no | Time-to-live as ISO 8601 duration (default: 72 hours) |

Response (200):

```json
{"id": "2025-07-14T22:31:05.123456789"}
```

## POST /in

Find and remove the earliest matching object. The caller
authenticates with the `X-ACE-Client-Key` header, which the
server resolves to an `ace:` identity for access control. If
`--insecure-ids` is enabled, a bare identity string may be
passed in `X-ACE-ID` instead. Both headers are optional; omit
them when objects have no access restrictions.

Request body:

```json
{
  "pattern": {"type": "task"},
  "wait": 10,
  "since": "2025-07-14T22:31:05.000000000"
}
```

| Field | Required | Purpose |
|-------|----------|---------|
| `pattern` | yes | JSON pattern (see `spec.md`) |
| `wait` | no | Maximum time to block: a JSON number (seconds), an integer string, an ISO 8601 duration string (e.g. `"PT10S"`), or a Go duration string (e.g. `"10s"`). Default: 0. |
| `since` | no | Only objects after this identifier |

The `pattern` field accepts the embeddings and LLM syntax
described in `spec.md`. `{"context~":"TexMex food"}` uses the
default cosine-distance threshold, and
`{"context~euclidean<1e-3":"TexMex food"}` selects an
explicit embeddings metric and threshold. `{"comment?":"TexMex food"}`
uses the server's configured LLM endpoint and model with the
built-in yes-or-no relation prompt. The server uses
`EMBEDDINGS_API_KEY` for embeddings and `LLM_API_KEY` for LLM
matching when those keys are set, otherwise `OPENAI_API_KEY`.
If the required key is unavailable, the server returns a 400
error explaining that the requested filtering mode is
unavailable.

Response when a match exists (200):

```json
{"id": "2025-07-14T22:31:05.123456789", "object": {"#id": "job-42", "type": "task", "payload": "compute"}}
```

Unmatched properties (like `#id`) are returned in the object
even though they do not participate in pattern matching.

When explicit deletes are enabled, the response includes a
`delete_id`:

```json
{"id": "2025-07-14T22:31:05.123456789", "object": {"#id": "job-42", "type": "task", "payload": "compute"}, "delete_id": "a1b2c3d4e5f6..."}
```

Response when no match exists (200):

```json
null
```

## POST /rd

`/rd` uses the same request and response format as `/in`.
The object remains in the space.

## POST/GET /match

Test whether an object matches a pattern. This operation does
not read from or write to the space.

POST request body:

```json
{"object": {"type": "task", "priority": 1}, "pattern": {"type": "task"}}
```

GET request: pass `object` and `pattern` as query parameters
containing URL-encoded JSON.

```
GET /match?object=%7B%22type%22%3A%22task%22%7D&pattern=%7B%22type%22%3A%22task%22%7D
```

| Field | Required | Purpose |
|-------|----------|---------|
| `object` | yes | JSON object |
| `pattern` | yes | JSON pattern (see `spec.md`) |

Response (200):

```json
{"match": true}
```

## POST/GET /del

Confirm deletion of an object previously returned by `/in`
with explicit deletes enabled. See `spec.md` for the explicit
deletes mechanism.

POST request body:

```json
{"delete_id": "a1b2c3d4e5f6..."}
```

GET request: pass `delete_id` as a query parameter.

```
GET /del?delete_id=a1b2c3d4e5f6...
```

| Field | Required | Purpose |
|-------|----------|---------|
| `delete_id` | yes | Deletion ID returned by `/in` |

Response (200):

```json
{"deleted": true}
```

Returns `{"deleted": false}` if the `delete_id` is invalid
or its visibility timeout has expired.

## GET /limits

Returns the active limits as a JSON object. See `spec.md`
for the list of properties and defaults.

## GET /stats

Returns storage statistics.

```json
{
  "objects": 42,
  "expired": 3,
  "branches": 84,
  "access_records": 10,
  "avg_branch_length": 12.5,
  "avg_branches_per_object": 2.0,
  "avg_access_in_per_object": 0.1,
  "avg_access_rd_per_object": 0.05
}
```

## POST /reg

Register a new client identity. See `spec.md` for the
identity model.

Request body (name is optional):

```json
{"name": "alice"}
```

| Field | Required | Purpose |
|-------|----------|---------|
| `name` | no | Human-readable name (1-20 alphanumeric, hyphen, or underscore characters) |

Response (200):

```json
{"key": "a1b2c3...64hex", "id": "ace:d4e5f6...64hex", "name": "acen:alice"}
```

If no name is given, `name` defaults to the `id` value.
Duplicate names return 400.

## GET /regcheck

Look up a registered identity by key, ID, or name. Provide
exactly one of the three lookup methods. The response
contains only the fields appropriate to the lookup method.

| Lookup by | Parameter | Returns |
|-----------|-----------|---------|
| Key | `X-ACE-Client-Key` header or `key` query parameter | `id` and `name` |
| ID | `id` query parameter | `name` |
| Name | `name` query parameter | `id` |

Example response for key lookup (200):

```json
{"id": "ace:d4e5f6...64hex", "name": "acen:alice"}
```

Returns 404 if the identity does not exist.

## GET /ping

Returns the git commit hash of the running server binary.
The hash is embedded at build time via `-ldflags`.

Response (200):

```json
{"commit": "a1b2c3d4e5f6..."}
```

The `commit` field is empty if the binary was built without
`-ldflags "-X github.com/morphism/ace.Commit=..."`.

## POST/GET /mcp

Remote MCP uses the Streamable HTTP transport on `/mcp`.
`POST /mcp` accepts one MCP JSON-RPC message in the request
body. Requests return `application/json` with a JSON-RPC
response. Notifications and client responses return 202 with
no body.

Clients should include:

```http
Accept: application/json, text/event-stream
Content-Type: application/json
MCP-Protocol-Version: 2025-06-18
```

If `--mcp-token` or `ACE_MCP_TOKEN` is set, clients must also
include:

```http
Authorization: Bearer TOKEN
```

When `--mcp-origins` is set, browser requests whose `Origin`
header is not listed return 403. `GET /mcp` returns 405
because ACE does not currently maintain an unsolicited
server-to-client SSE stream.

## GET /doc

Returns a plain-text index listing the embedded
documentation files.

```
ACE is a coordination service for software agents based on the tuple-space model.

  /doc/README.md
  /doc/docs/spec.md
  /doc/docs/http-spec.md
  /doc/docs/cli-spec.md
  /doc/docs/mcp-spec.md
  /doc/docs/guide.md
  /doc/docs/skill.md
```

## GET /doc/{name}

Returns the content of the named documentation file as
`text/plain; charset=utf-8`. Returns 404 for unknown names.

## Errors

Errors return an appropriate HTTP status code with a JSON
body:

```json
{"error": "object size is 3000 > 2048 bytes"}
```

| Status | Meaning |
|--------|---------|
| 400 | Invalid request: malformed JSON, missing fields, or limit violations |
| 405 | Wrong HTTP method for the endpoint |
| 429 | Rate limit exceeded (see `--throttle`) |
| 500 | Internal error |
| 503 | Too many waiting clients, or service updating during an automatic update |

## Max waiters

The server accepts a maximum concurrent waiters parameter.
When the number of blocked `/in` and `/rd` requests reaches
this limit, new blocking requests receive 503. Non-blocking
requests (those with `wait` absent or zero) pass through
regardless. A limit of zero means unlimited.
