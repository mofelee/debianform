package graph

import (
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

func aptRepositorySourcePath(name string) string {
	return "/etc/apt/sources.list.d/" + providerName(name) + ".sources"
}

func aptRepositorySourceContent(repo ir.APTRepositorySpec) string {
	lines := []string{
		"Types: deb",
		"URIs: " + strings.Join(repo.URIs, " "),
		"Suites: " + strings.Join(repo.Suites, " "),
		"Components: " + strings.Join(repo.Components, " "),
	}
	if len(repo.Architectures) > 0 {
		lines = append(lines, "Architectures: "+strings.Join(repo.Architectures, " "))
	}
	if repo.SigningKey != nil && repo.SigningKey.Path != "" {
		lines = append(lines, "Signed-By: "+repo.SigningKey.Path)
	}
	return strings.Join(lines, "\n") + "\n"
}
