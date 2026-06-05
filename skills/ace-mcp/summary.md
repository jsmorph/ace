# ACE Remote MCP Summary

ACE is for MCP clients that need to coordinate with other agents through shared JSON records: tasks, state, events, results, and access-controlled work are found by pattern rather than by a fixed queue. Use the remote MCP skill when an agent reaches ACE through `POST /mcp` with Streamable HTTP and JSON-RPC `tools/call` messages. Read the [ACE Remote MCP skill](SKILL.md) for required headers, initialization, complete JSON-RPC request bodies, tool arguments, identity use, waits, deletes, and result handling.
