package merge

import (
	"fmt"
	"sort"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func packageItems(packages parser.Value) ([]ir.PackageItem, error) {
	out := []ir.PackageItem{}
	seen := map[string]ir.SourceRef{}

	install, ok, err := listField(packages, "install")
	if err != nil {
		return nil, err
	}
	if ok {
		for _, item := range install.List {
			name, ok := item.StringValue()
			if !ok || name == "" {
				return nil, fmt.Errorf("%s:%d:%s: package install entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path)
			}
			if previous, exists := seen[name]; exists {
				return nil, fmt.Errorf("%s:%d:%s: duplicate package %q; first declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, name, previous.File, previous.Line, previous.Path)
			}
			seen[name] = item.Source
			out = append(out, ir.PackageItem{Name: name, Source: item.Source})
		}
	}

	packageBlocks, ok := packages.Map["package"]
	if !ok {
		return out, nil
	}
	if !packageBlocks.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: package must be a map", packageBlocks.Source.File, packageBlocks.Source.Line, packageBlocks.Source.Path)
	}
	names := make([]string, 0, len(packageBlocks.Map))
	for name := range packageBlocks.Map {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		item := packageBlocks.Map[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: package label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if previous, exists := seen[name]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate package %q; first declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, name, previous.File, previous.Line, previous.Path)
		}
		repositories, err := stringListField(item, "repositories")
		if err != nil {
			return nil, err
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		seen[name] = item.Source
		out = append(out, ir.PackageItem{Name: name, Repositories: repositories, Lifecycle: lifecycle, Source: item.Source})
	}
	return out, nil
}
