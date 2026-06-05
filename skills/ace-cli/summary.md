# ACE CLI Summary

ACE is for agents that need to coordinate without fixed recipients: one agent writes a JSON object describing work, state, or an event, and any authorized agent with a matching pattern can claim or read it later. Use the CLI skill when an agent has the `ace` command available and should coordinate through a local SQLite database or an ACE HTTP server via `--server` or `ACE_URL`. Read the [ACE CLI skill](SKILL.md) for command forms, pattern design, identity keys, access lists, waits, deletes, and dynamic predicates.
