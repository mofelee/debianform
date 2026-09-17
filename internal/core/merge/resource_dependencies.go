package merge

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

type resourceDeclaration struct {
	typ     string
	label   string
	address string
	value   parser.Value
}

func resourceDependencies(raw parser.Value, prefix string) ([]ir.ResourceDependencySpec, error) {
	declarations, err := dependencyDeclarations(raw, prefix)
	if err != nil {
		return nil, err
	}
	byReference := make(map[string]resourceDeclaration, len(declarations))
	for _, declaration := range declarations {
		byReference[resourceReferenceKey(declaration.typ, declaration.label)] = declaration
	}

	var out []ir.ResourceDependencySpec
	seen := map[string]struct{}{}
	for _, declaration := range declarations {
		dependsOn, ok := declaration.value.Map["depends_on"]
		if !ok {
			continue
		}
		if !dependsOn.IsList() {
			return nil, fmt.Errorf("%s:%d:%s: depends_on must be a list of resource references", dependsOn.Source.File, dependsOn.Source.Line, dependsOn.Source.Path)
		}
		for _, item := range dependsOn.List {
			if item.ResourceReference == nil {
				return nil, fmt.Errorf("%s:%d:%s: depends_on entry is not a typed resource reference", item.Source.File, item.Source.Line, item.Source.Path)
			}
			ref := *item.ResourceReference
			if ref.Type == "artifact" {
				key := declaration.address + "\x00artifact\x00" + ref.Name
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, ir.ResourceDependencySpec{From: declaration.address, ArtifactPath: ref.Name, Source: ref.Source})
				continue
			}
			target, exists := byReference[resourceReferenceKey(ref.Type, ref.Name)]
			if !exists {
				if ref.Type == "package" && listPackageDeclared(raw, ref.Name) {
					return nil, fmt.Errorf("%s:%d:%s: package.%s is list-form and cannot be referenced; convert it to a labeled package block", ref.Source.File, ref.Source.Line, ref.Source.Path, ref.Name)
				}
				return nil, fmt.Errorf("%s:%d:%s: depends_on references unknown %s", ref.Source.File, ref.Source.Line, ref.Source.Path, resourceReferenceDisplay(ref.Type, ref.Name))
			}
			key := declaration.address + "\x00" + target.address
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ir.ResourceDependencySpec{From: declaration.address, DependsOn: target.address, Source: ref.Source})
		}
	}
	return out, nil
}

// artifactInstallAddresses indexes host component artifact install addresses by
// installed path so cross-component artifact references can be resolved.
func artifactInstallAddresses(hostName string, components []ir.ComponentInstanceSpec) map[string][]string {
	index := map[string][]string{}
	for _, component := range components {
		if component.Install == nil || component.Install.Path == "" {
			continue
		}
		address := fmt.Sprintf("host.%s.components.%s.artifact.install[%s]", hostName, component.Name, strconv.Quote(component.Install.Path))
		index[component.Install.Path] = append(index[component.Install.Path], address)
	}
	return index
}

// resolveArtifactDependencies rewrites cross-component artifact references in
// place into concrete host-scoped artifact install addresses.
func resolveArtifactDependencies(dependencies []ir.ResourceDependencySpec, index map[string][]string) error {
	for i := range dependencies {
		path := dependencies[i].ArtifactPath
		if path == "" {
			continue
		}
		addresses := index[path]
		switch len(addresses) {
		case 0:
			return fmt.Errorf("%s:%d:%s: depends_on references unknown %s", dependencies[i].Source.File, dependencies[i].Source.Line, dependencies[i].Source.Path, resourceReferenceDisplay("artifact", path))
		case 1:
			dependencies[i].DependsOn = addresses[0]
			dependencies[i].ArtifactPath = ""
		default:
			return fmt.Errorf("%s:%d:%s: depends_on references ambiguous %s; installed by %s", dependencies[i].Source.File, dependencies[i].Source.Line, dependencies[i].Source.Path, resourceReferenceDisplay("artifact", path), strings.Join(addresses, ", "))
		}
	}
	return nil
}

func dependencyDeclarations(raw parser.Value, prefix string) ([]resourceDeclaration, error) {
	var out []resourceDeclaration
	for _, spec := range []struct {
		domain     string
		collection string
		typ        string
	}{
		{domain: "packages", collection: "package", typ: "package"},
		{domain: "files", collection: "file", typ: "file"},
		{domain: "services", collection: "service", typ: "service"},
	} {
		domain, ok := raw.Map[spec.domain]
		if !ok {
			continue
		}
		objects, ok, err := objectCollection(domain, spec.collection)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, label := range sortedKeys(objects) {
			value := objects[label]
			identity := label
			addressCollection := spec.collection
			switch spec.typ {
			case "package":
				addressCollection = "install"
			case "file":
				identity, err = objectPath(value, "path", label)
				if err != nil {
					return nil, err
				}
			case "service":
				identity, err = objectName(value, "name", label)
				if err != nil {
					return nil, err
				}
			}
			address := fmt.Sprintf("%s.%s.%s[%s]", prefix, spec.domain, addressCollection, strconv.Quote(identity))
			out = append(out, resourceDeclaration{typ: spec.typ, label: label, address: address, value: value})
		}
	}
	return out, nil
}

func listPackageDeclared(raw parser.Value, name string) bool {
	packages, ok := raw.Map["packages"]
	if !ok {
		return false
	}
	install, ok := packages.Map["install"]
	if !ok || !install.IsList() {
		return false
	}
	for _, item := range install.List {
		if value, ok := item.StringValue(); ok && value == name {
			return true
		}
	}
	return false
}

func resourceReferenceKey(typ, name string) string {
	return typ + "\x00" + name
}

func resourceReferenceDisplay(typ, name string) string {
	if hclIdentifier(name) {
		return typ + "." + name
	}
	return fmt.Sprintf("%s[%s]", typ, strconv.Quote(name))
}

func hclIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '-' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
