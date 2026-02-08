# Pattern extensions

ACE uses [Quamina](https://github.com/timbray/quamina) for
content-based pattern matching in the blocking notification
path. Upstream Quamina supports equality, existence, prefix,
wildcard, anything-but, case-insensitive equality, and
regular expressions. It does not support numeric or string
inequalities.

A [fork](https://github.com/jsmorph/quamina/tree/extensions)
adds an extension framework and implementations for numeric
and string comparisons. These notes document that work for
future integration with ACE.

## Extension framework

The fork introduces a predicate-based extension mechanism. A
`Predicate` is a `func([]byte) bool` that evaluates whether a
value satisfies a custom constraint. A `PredicateParser`
converts a JSON specification into a predicate at pattern
compilation time. When Quamina encounters an `"extension"`
field in a pattern leaf, it delegates to the registered
parser.

A pattern using extensions looks like:

```json
{"price": [{"extension": {"numeric": ">= 5, < 10"}}]}
```

Extensions are slower than Quamina's native automata-based
matching because each candidate value invokes a function call
rather than traversing a precompiled state machine.

## Numeric relations

Seven relations on float64 values:

| Operator | Meaning |
|----------|---------|
| `<` | Less than |
| `>` | Greater than |
| `<=` | Less than or equal |
| `>=` | Greater than or equal |
| `=` | Equal |
| `!=` | Not equal |
| `~=` | Approximate equality (within `1e-08`) |

Multiple constraints combine with AND, so `">= 5, < 10"`
matches values in the half-open interval [5, 10).

## String relations

The same seven operators applied lexicographically. No
prefix, suffix, or substring matching: only ordering
comparisons.

## Integration with ACE

To use these extensions in ACE, two things would need to
happen. First, the notification path (notifier.go) would need
to call `UseStdExtension()` and use `AddExtendedPattern()`
when registering patterns with Quamina. Second, the SQL query
path (query.go) would need equivalent WHERE clauses: numeric
comparisons map to SQL `<`, `>`, etc., and string comparisons
map to the same operators on TEXT columns.

The extension predicate mechanism handles the Quamina side.
The SQL side requires translating extension constraints into
parameterized query fragments, which is straightforward for
these operators.
