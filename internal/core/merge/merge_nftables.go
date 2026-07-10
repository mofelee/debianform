package merge

import (
	"fmt"
	"path/filepath"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func nftablesSpec(nftables parser.Value) (ir.NftablesSpec, error) {
	spec := ir.NftablesSpec{
		Files:  map[string]ir.NftablesFileSpec{},
		Source: nftables.Source,
	}
	if enable, ok, err := boolField(nftables, "enable"); err != nil {
		return spec, err
	} else if ok {
		value := enable
		spec.Enable = &value
	}
	if main, ok, err := mapField(nftables, "main"); err != nil {
		return spec, err
	} else if ok {
		compiled, err := nftablesFileSpec("main", main, "/etc/nftables.conf")
		if err != nil {
			return spec, err
		}
		spec.Main = &compiled
	}
	objects, ok, err := objectCollection(nftables, "file")
	if err != nil {
		return spec, err
	}
	if ok {
		for _, label := range sortedKeys(objects) {
			item := objects[label]
			if label == "" {
				return spec, fmt.Errorf("%s:%d:%s: nftables file label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			compiled, err := nftablesFileSpec(label, item, "/etc/nftables.d/"+label+".nft")
			if err != nil {
				return spec, err
			}
			spec.Files[label] = compiled
		}
	}
	return spec, nil
}

func nftablesFileSpec(label string, item parser.Value, defaultPath string) (ir.NftablesFileSpec, error) {
	path, hasPath, err := stringField(item, "path")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	if !hasPath || path == "" {
		path = defaultPath
	}
	if !filepath.IsAbs(path) {
		return ir.NftablesFileSpec{}, fmt.Errorf("%s:%d:%s.path: nftables file path must be absolute", item.Source.File, item.Source.Line, item.Source.Path)
	}
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	content, hasContent, err := stringField(item, "content")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	if hasContent && hasSource {
		return ir.NftablesFileSpec{}, fmt.Errorf("%s:%d:%s: nftables file requires exactly one of content or source", item.Source.File, item.Source.Line, item.Source.Path)
	}
	if ensure == "present" && !hasContent && !hasSource {
		return ir.NftablesFileSpec{}, fmt.Errorf("%s:%d:%s: nftables file requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	sensitive, ok, err := boolField(item, "sensitive")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	if !ok {
		sensitive = false
	}
	if hasContent && item.Map["content"].ContainsSensitive() {
		sensitive = true
	}
	validate, err := boolFieldDefault(item, "validate", true)
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	activate, err := boolFieldDefault(item, "activate", true)
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	file := ir.NftablesFileSpec{
		Label:      label,
		Path:       path,
		Content:    content,
		SourcePath: resolvePath(item.Source.File, sourcePath),
		Owner:      owner,
		Group:      group,
		Mode:       mode,
		Sensitive:  sensitive,
		Validate:   validate,
		Activate:   activate,
		Ensure:     ensure,
		Lifecycle:  lifecycle,
		Source:     item.Source,
	}
	if hasContent {
		file.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(file.SourcePath, item.Source)
		if err != nil {
			return ir.NftablesFileSpec{}, err
		}
		file.Summary = summary
	}
	return file, nil
}
