package ace

import "embed"

//go:embed README.md docs/spec.md docs/http-spec.md docs/cli-spec.md docs/guide.md docs/skill.md
var Docs embed.FS

var DocFiles = []string{
	"README.md",
	"docs/spec.md",
	"docs/http-spec.md",
	"docs/cli-spec.md",
	"docs/guide.md",
	"docs/skill.md",
}
