package ace

import "embed"

//go:embed README.md spec.md http-spec.md cli-spec.md guide.md skill.md
var Docs embed.FS

var DocFiles = []string{
	"README.md",
	"spec.md",
	"http-spec.md",
	"cli-spec.md",
	"guide.md",
	"skill.md",
}
