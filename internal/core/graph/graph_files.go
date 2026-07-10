package graph

import (
	"github.com/mofelee/debianform/internal/core/ir"
)

func optionalContentSummary(content string) ir.ContentSummary {
	if content == "" {
		return ir.ContentSummary{}
	}
	return contentSummary([]byte(content))
}

type fileResourceSpec struct {
	Path             string
	Component        string
	Content          string
	ContentVersion   string
	ContentWriteOnly bool
	SourcePath       string
	Owner            string
	Group            string
	Mode             string
	Sensitive        bool
	Ensure           string
	Summary          ir.ContentSummary
}

func fileResourceDesiredPayload(item fileResourceSpec) (map[string]any, map[string]any) {
	desired := map[string]any{
		"path":      item.Path,
		"owner":     item.Owner,
		"group":     item.Group,
		"mode":      item.Mode,
		"ensure":    item.Ensure,
		"sensitive": item.Sensitive,
	}
	if item.Component != "" {
		desired["component"] = item.Component
	}
	if !item.ContentWriteOnly {
		desired["summary"] = item.Summary
	}
	if item.ContentWriteOnly {
		desired["content_write_only"] = true
	}
	if item.ContentVersion != "" {
		desired["content_version"] = item.ContentVersion
	}
	if item.Content != "" && !item.Sensitive {
		desired["content"] = item.Content
	}
	if item.Content != "" && item.Sensitive && !item.ContentWriteOnly {
		desired["content_sha256"] = item.Summary.SHA256
		desired["content_bytes"] = item.Summary.Bytes
	}
	if item.SourcePath != "" {
		desired["source_path"] = item.SourcePath
	}
	payload := cloneMap(desired)
	if item.Content != "" {
		payload["content"] = item.Content
	}
	if item.SourcePath != "" {
		payload["source_path"] = item.SourcePath
	}
	return desired, payload
}
