package merge

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func aptRepositorySpecs(apt parser.Value) (map[string]ir.APTRepositorySpec, error) {
	objects, ok, err := objectCollection(apt, "repository")
	if err != nil || !ok {
		return map[string]ir.APTRepositorySpec{}, err
	}
	out := make(map[string]ir.APTRepositorySpec, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: apt repository label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		var uris, suites, components []string
		if ensure == "present" {
			uris, err = requiredStringListField(item, "uris")
			if err != nil {
				return nil, err
			}
			suites, err = requiredStringListField(item, "suites")
			if err != nil {
				return nil, err
			}
			components, err = requiredStringListField(item, "components")
			if err != nil {
				return nil, err
			}
		} else {
			uris, err = stringListField(item, "uris")
			if err != nil {
				return nil, err
			}
			suites, err = stringListField(item, "suites")
			if err != nil {
				return nil, err
			}
			components, err = stringListField(item, "components")
			if err != nil {
				return nil, err
			}
		}
		architectures, err := stringListField(item, "architectures")
		if err != nil {
			return nil, err
		}
		signingKey, err := aptSigningKeySpec(name, item, ensure)
		if err != nil {
			return nil, err
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[name] = ir.APTRepositorySpec{
			Name:          name,
			URIs:          uris,
			Suites:        suites,
			Components:    components,
			Architectures: architectures,
			SigningKey:    signingKey,
			Ensure:        ensure,
			Lifecycle:     lifecycle,
			Source:        item.Source,
		}
	}
	return out, nil
}

func aptSourceFileSpecs(apt parser.Value) (map[string]ir.APTSourceFileSpec, error) {
	objects, ok, err := objectCollection(apt, "source_file")
	if err != nil || !ok {
		return map[string]ir.APTSourceFileSpec{}, err
	}
	out := make(map[string]ir.APTSourceFileSpec, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		if label == "" {
			return nil, fmt.Errorf("%s:%d:%s: apt source_file label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		path, ok, err := stringField(item, "path")
		if err != nil {
			return nil, err
		}
		if !ok || path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s.path: apt source_file path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		content, hasContent, err := stringField(item, "content")
		if err != nil {
			return nil, err
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && hasContent == hasSource {
			return nil, fmt.Errorf("%s:%d:%s: apt.source_file requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		owner, err := stringFieldDefault(item, "owner", "root")
		if err != nil {
			return nil, err
		}
		group, err := stringFieldDefault(item, "group", "root")
		if err != nil {
			return nil, err
		}
		mode, err := modeFieldDefault(item, "mode", "0644")
		if err != nil {
			return nil, err
		}
		onDestroy, err := stringFieldDefault(item, "on_destroy", "keep")
		if err != nil {
			return nil, err
		}
		if onDestroy != "keep" && onDestroy != "restore" {
			return nil, fmt.Errorf("%s:%d:%s.on_destroy: on_destroy must be keep or restore", item.Source.File, item.Source.Line, item.Source.Path)
		}
		sensitive := hasContent && item.Map["content"].ContainsSensitive()
		if sensitive && onDestroy == "restore" {
			return nil, fmt.Errorf("%s:%d:%s.on_destroy: restore is not supported with sensitive content", item.Source.File, item.Source.Line, item.Source.Path)
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		sourceFile := ir.APTSourceFileSpec{
			Label:      label,
			Path:       path,
			Content:    content,
			SourcePath: resolvePath(item.Source.File, sourcePath),
			Owner:      owner,
			Group:      group,
			Mode:       mode,
			Ensure:     ensure,
			OnDestroy:  onDestroy,
			Sensitive:  sensitive,
			Lifecycle:  lifecycle,
			Source:     item.Source,
		}
		if hasContent {
			sourceFile.Summary = contentSummary([]byte(content))
		} else if hasSource {
			summary, err := fileSummary(sourceFile.SourcePath, item.Source)
			if err != nil {
				return nil, err
			}
			sourceFile.Summary = summary
		}
		out[label] = sourceFile
	}
	return out, nil
}

func aptSigningKeySpec(repoName string, repo parser.Value, ensure string) (*ir.APTSigningKeySpec, error) {
	signingKey, ok, err := mapField(repo, "signing_key")
	if err != nil || !ok {
		return nil, err
	}
	url, hasURL, err := stringField(signingKey, "url")
	if err != nil {
		return nil, err
	}
	content, hasContent, err := stringField(signingKey, "content")
	if err != nil {
		return nil, err
	}
	sha, hasSHA, err := stringField(signingKey, "sha256")
	if err != nil {
		return nil, err
	}
	path, hasPath, err := stringField(signingKey, "path")
	if err != nil {
		return nil, err
	}
	if hasURL && url == "" {
		return nil, fmt.Errorf("%s:%d:%s.url: signing key url must be non-empty", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasContent && content == "" {
		return nil, fmt.Errorf("%s:%d:%s.content: signing key content must be non-empty", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasURL && hasContent {
		return nil, fmt.Errorf("%s:%d:%s: signing_key requires exactly one of url or content", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if ensure == "present" && !hasURL && !hasContent {
		return nil, fmt.Errorf("%s:%d:%s: signing_key requires exactly one of url or content when repository ensure is present", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasURL && (!hasSHA || sha == "") {
		return nil, fmt.Errorf("%s:%d:%s.sha256: signing key url requires sha256", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasSHA && sha != "" {
		if !sha256Pattern.MatchString(sha) {
			return nil, fmt.Errorf("%s:%d:%s.sha256: sha256 must be a 64 character hex string", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
		}
		sha = strings.ToLower(sha)
		if hasContent && sha != contentSummary([]byte(content)).SHA256 {
			return nil, fmt.Errorf("%s:%d:%s.sha256: sha256 does not match signing key content", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
		}
	}
	if !hasPath || path == "" {
		path = defaultAPTSigningKeyPath(repoName)
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s:%d:%s.path: signing key path must be absolute", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	return &ir.APTSigningKeySpec{
		URL:       url,
		Content:   content,
		SHA256:    sha,
		Path:      path,
		Sensitive: hasContent && signingKey.Map["content"].ContainsSensitive(),
		Source:    signingKey.Source,
	}, nil
}

func defaultAPTSigningKeyPath(name string) string {
	return "/etc/apt/keyrings/" + safeAPTName(name) + ".asc"
}

func safeAPTName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "repository"
	}
	return strings.ToLower(out)
}
