# ACE: Agent Coordination Engine

ACE is a coordination service for software agents. Agents
communicate by writing JSON objects into a shared persistent store
and retrieving them by pattern. A producer does not name a
recipient; a consumer does not name a source. Agents coordinate
through the content of the objects themselves.

ACE exposes an HTTP API, a CLI, local MCP over stdio, and
remote MCP over Streamable HTTP. The HTTP API allows any
number of agents to coordinate across networks through a
shared server. The CLI operates directly on the local
database or, with `--server`, against a remote ACE instance,
while MCP exposes the same core operations as tools.

## Operations

`out` writes a JSON object into the space. Each object receives a
unique timestamp identifier. Objects can carry access control
restrictions and a time-to-live.

`in` finds and removes the earliest object matching a pattern. If
no match exists, `in` can block for a specified duration until one
appears. This is how an agent claims a task.

`rd` finds the earliest matching object without removing it. This
is how an agent observes state without consuming it.

`del` confirms deletion of an object when explicit deletes are
enabled. In this mode, `in` marks objects invisible rather than
removing them, and the caller must confirm within a timeout. If
confirmation does not arrive, the object reappears.

## Pattern matching

A pattern is a JSON object that specifies required fields. An
object matches when every field in the pattern appears in the
object with a matching value. Extra fields in the object do not
prevent a match. The empty pattern `{}` matches every object.

An array value in a pattern means "any of these":
`{"status":["pending","retry"]}` matches an object whose `status`
is `"pending"` or `"retry"`. Patterns support nested objects. Full
rules are in `spec.md`.

## Access control

An object can restrict which callers may read or consume it. The
`access` parameter on `out` accepts lists of permitted caller
identities for `in` and `rd` independently. Callers authenticate
with the `X-ACE-Client-Key` header (or `--key` on the CLI), which
the server resolves to an `ace:` identity. Register identities with
`POST /reg` (or `ace reg`). Objects without access restrictions are
available to everyone.

## TTL

Every object has a time-to-live (default: 72 hours, maximum:
7 days). Expired objects become invisible.

## Documentation

Full specifications are embedded in the ACE binary and served
at `/doc`:

| Path | Content |
|------|---------|
| `/doc` | Index of available documents |
| `/doc/docs/spec.md` | Operations, pattern matching, access, TTL, limits |
| `/doc/docs/http-spec.md` | HTTP endpoints, request/response formats, error codes |
| `/doc/docs/cli-spec.md` | CLI subcommands and flags |
| `/doc/docs/guide.md` | Worked examples for common coordination patterns |
| `/doc/docs/skill.md` | This document |

From the command line: `ace doc` lists documents,
`ace doc spec.md` prints one.
