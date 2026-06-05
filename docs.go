package ace

import "embed"

// Commit is the git commit hash, set at build time via ldflags.
var Commit string

//go:embed README.md docs/spec.md docs/http-spec.md docs/cli-spec.md docs/mcp-spec.md docs/guide.md skills/ace-cli/summary.md skills/ace-cli/SKILL.md skills/ace-netapi/summary.md skills/ace-netapi/SKILL.md skills/ace-mcp/summary.md skills/ace-mcp/SKILL.md
var Docs embed.FS

var DocFiles = []string{
	"README.md",
	"docs/spec.md",
	"docs/http-spec.md",
	"docs/cli-spec.md",
	"docs/mcp-spec.md",
	"docs/guide.md",
	"skills/ace-cli/summary.md",
	"skills/ace-cli/SKILL.md",
	"skills/ace-netapi/summary.md",
	"skills/ace-netapi/SKILL.md",
	"skills/ace-mcp/summary.md",
	"skills/ace-mcp/SKILL.md",
}
