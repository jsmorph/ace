package ace

import "embed"

// Commit is the git commit hash, set at build time via ldflags.
var Commit string

//go:embed README.md docs/spec.md docs/http-spec.md docs/cli-spec.md docs/mcp-spec.md docs/guide.md docs/skill.md
var Docs embed.FS

var DocFiles = []string{
	"README.md",
	"docs/spec.md",
	"docs/http-spec.md",
	"docs/cli-spec.md",
	"docs/mcp-spec.md",
	"docs/guide.md",
	"docs/skill.md",
}
