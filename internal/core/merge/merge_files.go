package merge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func fileSpecs(files parser.Value) (map[string]ir.ManagedFile, error) {
	objects, ok, err := objectCollection(files, "file")
	if err != nil || !ok {
		return map[string]ir.ManagedFile{}, err
	}
	out := make(map[string]ir.ManagedFile, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		path, err := objectPath(item, "path", label)
		if err != nil {
			return nil, err
		}
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: file path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if previous, exists := out[path]; exists {
			return nil, fmt.Errorf("%s:%d:%s: file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		content, hasContent, err := stringFieldAllowEphemeral(item, "content")
		if err != nil {
			return nil, err
		}
		contentVersion, hasContentVersion, err := stringField(item, "content_version")
		if err != nil {
			return nil, err
		}
		if hasContentVersion && item.Map["content_version"].ContainsSensitive() {
			value := item.Map["content_version"]
			return nil, fmt.Errorf("%s:%d:%s: content_version must not be sensitive", value.Source.File, value.Source.Line, value.Source.Path)
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && hasContent == hasSource {
			return nil, fmt.Errorf("%s:%d:%s: files.file requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		contentWriteOnly := hasContent && item.Map["content"].ContainsEphemeral()
		if contentWriteOnly && (!hasContentVersion || contentVersion == "") {
			return nil, fmt.Errorf("%s:%d:%s.content_version: files.file with ephemeral content requires content_version", item.Source.File, item.Source.Line, item.Source.Path)
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
		sensitive, ok, err := boolField(item, "sensitive")
		if err != nil {
			return nil, err
		}
		if !ok {
			sensitive = false
		}
		if hasContent && contentNeedsRedaction(item.Map["content"]) {
			sensitive = true
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		onChange, hasOnChange, err := stringField(item, "on_change")
		if err != nil {
			return nil, err
		}
		managed := ir.ManagedFile{
			Path:             path,
			Content:          content,
			ContentVersion:   contentVersion,
			ContentWriteOnly: contentWriteOnly,
			SourcePath:       resolvePath(item.Source.File, sourcePath),
			Owner:            owner,
			Group:            group,
			Mode:             mode,
			Sensitive:        sensitive,
			Ensure:           ensure,
			OnChange:         onChange,
			Lifecycle:        lifecycle,
			Source:           item.Source,
		}
		if hasOnChange {
			source := item.Map["on_change"].Source
			managed.OnChangeSource = &source
		}
		if hasContent {
			managed.Summary = contentSummary([]byte(content))
		} else if hasSource {
			summary, err := fileSummary(managed.SourcePath, item.Source)
			if err != nil {
				return nil, err
			}
			managed.Summary = summary
		}
		out[path] = managed
	}
	return out, nil
}

func rejectHostFileOnChange(files map[string]ir.ManagedFile) error {
	for _, path := range sortedKeys(files) {
		file := files[path]
		if file.OnChange == "" {
			continue
		}
		source := file.Source
		if file.OnChangeSource != nil {
			source = *file.OnChangeSource
		}
		if source.Path == "" {
			source = file.Source
		}
		return fmt.Errorf("%s:%d:%s: files.file.on_change is only supported inside component", source.File, source.Line, source.Path)
	}
	return nil
}

func validateFileOnChangeRefs(files map[string]ir.ManagedFile, scripts map[string]ir.ComponentScriptSpec) error {
	for _, path := range sortedKeys(files) {
		file := files[path]
		if file.OnChange == "" {
			continue
		}
		if _, ok := scripts[file.OnChange]; !ok {
			source := file.Source
			if file.OnChangeSource != nil {
				source = *file.OnChangeSource
			}
			if source.Path == "" {
				source = file.Source
			}
			return fmt.Errorf("%s:%d:%s: files.file.on_change references unknown script.%s", source.File, source.Line, source.Path, file.OnChange)
		}
	}
	return nil
}

func secretSpecs(secrets parser.Value) (map[string]ir.SecretFile, error) {
	objects, ok, err := objectCollection(secrets, "file")
	if err != nil || !ok {
		return map[string]ir.SecretFile{}, err
	}
	out := make(map[string]ir.SecretFile, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		path, err := objectPath(item, "path", label)
		if err != nil {
			return nil, err
		}
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: secret path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if previous, exists := out[path]; exists {
			return nil, fmt.Errorf("%s:%d:%s: secret path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && !hasSource {
			return nil, fmt.Errorf("%s:%d:%s: secrets.file requires source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		owner, err := stringFieldDefault(item, "owner", "root")
		if err != nil {
			return nil, err
		}
		group, err := stringFieldDefault(item, "group", "root")
		if err != nil {
			return nil, err
		}
		mode, err := modeFieldDefault(item, "mode", "0600")
		if err != nil {
			return nil, err
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		secret := ir.SecretFile{
			Path:       path,
			SourcePath: resolvePath(item.Source.File, sourcePath),
			Owner:      owner,
			Group:      group,
			Mode:       mode,
			Ensure:     ensure,
			Lifecycle:  lifecycle,
			Source:     item.Source,
		}
		if hasSource {
			summary, err := fileSummary(secret.SourcePath, item.Source)
			if err != nil {
				return nil, err
			}
			secret.Summary = summary
		}
		out[path] = secret
	}
	return out, nil
}

func directorySpecs(directories parser.Value) (map[string]ir.ManagedDirectory, error) {
	objects, ok, err := objectCollection(directories, "directory")
	if err != nil || !ok {
		return map[string]ir.ManagedDirectory{}, err
	}
	out := make(map[string]ir.ManagedDirectory, len(objects))
	for _, path := range sortedKeys(objects) {
		item := objects[path]
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: directory path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		owner, err := stringFieldDefault(item, "owner", "root")
		if err != nil {
			return nil, err
		}
		group, err := stringFieldDefault(item, "group", "root")
		if err != nil {
			return nil, err
		}
		mode, err := modeFieldDefault(item, "mode", "0755")
		if err != nil {
			return nil, err
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[path] = ir.ManagedDirectory{Path: path, Owner: owner, Group: group, Mode: mode, Ensure: ensure, Lifecycle: lifecycle, Source: item.Source}
	}
	return out, nil
}

func resolvePath(sourceFile string, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(filepath.Dir(sourceFile), value)
}

func fileSummary(path string, source ir.SourceRef) (ir.ContentSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ir.ContentSummary{}, fmt.Errorf("%s:%d:%s: read source %q: %w", source.File, source.Line, source.Path, path, err)
	}
	return contentSummary(data), nil
}

func contentSummary(data []byte) ir.ContentSummary {
	sum := sha256.Sum256(data)
	return ir.ContentSummary{SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
}
