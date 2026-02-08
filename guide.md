# Usage scenarios

These examples show how agents coordinate through ACE in
three settings: local software development, research, and a
shared business workflow with access control. All examples
use the CLI. The HTTP API accepts the same objects and
patterns.

Object values are limited to 128 bytes by default. ACE is a
coordination service, not a content store: objects carry
enough structure for pattern matching, and agents that need
to exchange large payloads can store them elsewhere and
reference them by URL.

## Software development

A developer runs an orchestrator agent that decomposes a
coding task into subtasks. Worker agents claim subtasks and
produce results. A reviewer agent checks each result.

The orchestrator posts two subtasks. Properties prefixed with
`#` are unmatched: stored and returned to the consumer but
excluded from pattern matching. The `#instructions` and
`#label` fields below carry free text that no agent would
match against.

```
ace out --object '{"type":"code-task","#label":"implement parser","module":"parser","language":"go","#instructions":"Write a recursive descent parser for the expression grammar in grammar.md"}'

ace out --object '{"type":"code-task","#label":"implement evaluator","module":"evaluator","language":"go","#instructions":"Write an evaluator that walks the AST produced by the parser"}'
```

A worker agent claims any available Go coding task:

```
ace in --pattern '{"type":"code-task","language":"go"}'
```

Output:

```json
{"id":"2025-07-14T10:00:00.000000001","object":{"#instructions":"Write a recursive descent parser for the expression grammar in grammar.md","#label":"implement parser","language":"go","module":"parser","type":"code-task"}}
```

The `in` operation consumed (removed) this object. The
remaining task for the evaluator is still in the space. Note
that keys in the returned object are in lexicographic order:
ACE canonicalizes all objects on storage.

The worker completes the task and posts a result. The `task`
field references the original task ID, so the orchestrator
can match results to tasks:

```
ace out --object '{"type":"code-result","task":"2025-07-14T10:00:00.000000001","module":"parser","status":"complete","files":["parser.go","parser_test.go"]}'
```

The reviewer agent reads results without consuming them, so
other agents can also observe progress:

```
ace rd --pattern '{"type":"code-result","module":"parser"}'
```

Another agent searches for any result that touches a specific
file. Because `files` is an array, the pattern matches any
object where `"parser.go"` appears as an element:

```
ace rd --pattern '{"type":"code-result","files":"parser.go"}'
```

After reviewing, the reviewer posts a verdict:

```
ace out --object '{"type":"review","module":"parser","verdict":"approved","#notes":"Tests pass. Parser handles all grammar productions."}'
```

The orchestrator can iterate over all review verdicts using
`since`. It reads the first one:

```
ace rd --pattern '{"type":"review"}'
```

This returns the verdict with the smallest ID. To read the
next one, pass the previous ID as `--since`:

```
ace rd --pattern '{"type":"review"}' \
  --since 2025-07-14T10:05:00.000000000
```

## Research

A research coordinator posts questions. Searcher agents find
relevant sources. An analyst agent reads the collected
sources and produces a summary.

The coordinator posts a question with a one-day TTL:

```
ace out --object '{"type":"question","topic":"sqlite-wal","#question":"What are the tradeoffs of WAL mode vs DELETE mode in SQLite for single-writer workloads?"}' --ttl P1D
```

Two searcher agents work independently. Each finds sources
and posts them:

```
ace out --object '{"type":"source","topic":"sqlite-wal","#title":"SQLite WAL mode documentation","url":"https://sqlite.org/wal.html","#summary":"Official documentation covering WAL advantages and limitations"}' --ttl P1D

ace out --object '{"type":"source","topic":"sqlite-wal","#title":"Write-Ahead Logging","url":"https://sqlite.org/draft/wal.html","#summary":"Technical description of the WAL file format and checkpoint mechanism"}' --ttl P1D
```

The analyst reads all sources for the topic without consuming
them. It reads the first:

```
ace rd --pattern '{"type":"source","topic":"sqlite-wal"}'
```

Then iterates with `--since` to read the rest. When finished,
it posts a summary. The full analysis is too long for an
object value, so the analyst writes it to a shared location
and stores the URL:

```
ace out --object '{"type":"summary","topic":"sqlite-wal","url":"s3://research/sqlite-wal/summary.md"}'
```

The coordinator retrieves the summary:

```
ace in --pattern '{"type":"summary","topic":"sqlite-wal"}'
```

Because sources were posted with `--ttl P1D`, they expire
after one day. The coordinator can also run `ace expire` to
clean up immediately.

## Business workflow with access control

An intake agent posts customer requests. Only authorized
processing agents can claim them. A compliance agent can read
any request without consuming it.

These examples use bare identity strings for clarity. Start
the server with `--insecure-ids` to allow bare `--id` values
and unprefixed access list entries:

```
ace serve --db shared.db --insecure-ids
```

In production, clients register with `ace reg` and
authenticate with `--key` instead of `--id`.

The intake agent posts a request. The `access` parameter
restricts who can consume or read it. The `#description`
field is unmatched: passed through to the processing agent but
not matchable, since it contains free text.

```
ace out --server http://localhost:8000 \
  --object '{"type":"request","ticket":"TK-4821","department":"billing","priority":"high","#description":"Customer reports duplicate charge on invoice 9917"}' \
  --access '{"in":["billing-agent","escalation-agent"],"rd":["compliance-agent","billing-agent","escalation-agent"]}'
```

Only `billing-agent` and `escalation-agent` can consume this
request. The `compliance-agent` can read it but not remove
it.

The compliance agent reads all pending requests to verify
they have been logged:

```
ace rd --server http://localhost:8000 --id compliance-agent \
  --pattern '{"type":"request"}'
```

The billing agent claims the request:

```
ace in --server http://localhost:8000 --id billing-agent \
  --pattern '{"type":"request","department":"billing"}'
```

The escalation agent handles high-priority requests across
departments. An array in the pattern matches any of the
listed values:

```
ace in --server http://localhost:8000 --id escalation-agent \
  --pattern '{"type":"request","priority":"high","department":["billing","support","shipping"]}'
```

An unauthorized agent gets nothing:

```
ace in --server http://localhost:8000 --id marketing-agent \
  --pattern '{"type":"request","department":"billing"}'
```

Output: `null`. The marketing agent cannot see billing
requests.

After resolving the issue, the billing agent posts a
resolution that anyone can read:

```
ace out --server http://localhost:8000 \
  --object '{"type":"resolution","ticket":"TK-4821","department":"billing","#outcome":"Refund issued for duplicate charge"}'
```

The compliance agent reads the resolution by ticket number:

```
ace rd --server http://localhost:8000 --id compliance-agent \
  --pattern '{"type":"resolution","ticket":"TK-4821"}'
```
