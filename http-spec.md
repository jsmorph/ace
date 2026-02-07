# ACE HTTP API

This document specifies the HTTP interface to ACE. See `spec.md` for the core operations, pattern matching, access control, TTL, blocking, and limits.

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/out` | Write an object |
| POST | `/in` | Read and remove a matching object |
| POST | `/rd` | Read a matching object |
| GET | `/limits` | Return the active limits |
| GET | `/stats` | Return storage statistics |

All request and response bodies use JSON with content type `application/json`.

## POST /out

Write an object into the space.

Request body:

```json
{
  "object": {"type": "task", "payload": "compute"},
  "access": {"in": ["worker-1"], "rd": ["monitor"]},
  "ttl": "P1D"
}
```

| Field | Required | Purpose |
|-------|----------|---------|
| `object` | yes | JSON object to store |
| `access` | no | Access control (see `spec.md`) |
| `ttl` | no | Time-to-live as ISO 8601 duration (default: 72 hours) |

Response (200):

```json
{"id": "2025-07-14T22:31:05.123456789"}
```

## POST /in

Find and remove the earliest matching object. The caller identifies itself with the `X-ACE-ID` header for access control. The header is optional; omit it when objects have no access restrictions.

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
| `wait` | no | Maximum seconds to block (default: 0) |
| `since` | no | Return only objects after this identifier |

Response when a match exists (200):

```json
{"id": "2025-07-14T22:31:05.123456789", "object": {"type": "task", "payload": "compute"}}
```

Response when no match exists (200):

```json
null
```

## POST /rd

`/rd` uses the same request and response format as `/in`. The object remains in the space.

## GET /limits

Returns the active limits as a JSON object. See `spec.md` for the list of properties and defaults.

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

## Errors

Errors return an appropriate HTTP status code with a JSON body:

```json
{"error": "object size is 3000 > 2048 bytes"}
```

| Status | Meaning |
|--------|---------|
| 400 | Invalid request: malformed JSON, missing required fields, or limit violations |
| 405 | Wrong HTTP method for the endpoint |
| 500 | Internal error |
| 503 | Too many waiting clients (see max waiters) |

## Max waiters

The server accepts a maximum concurrent waiters parameter. When the number of blocked `/in` and `/rd` requests reaches this limit, new blocking requests receive 503. Non-blocking requests (those with `wait` absent or zero) pass through regardless. A limit of zero means unlimited.
