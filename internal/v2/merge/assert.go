package merge

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/parser"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

func evaluateAssertions(asserts []parser.Assert, host ir.HostSpec) error {
	if len(asserts) == 0 {
		return nil
	}
	self := hostSpecToCty(host)
	for _, assert := range asserts {
		value, diags := assert.Condition.Value(&hcl.EvalContext{
			Variables: map[string]cty.Value{
				"self": self,
			},
			Functions: map[string]function.Function{
				"contains": containsFunction(),
			},
		})
		if diags.HasErrors() {
			return fmt.Errorf("%s:%d:%s: assert condition: %s", assert.ConditionSource.File, assert.ConditionSource.Line, assert.ConditionSource.Path, diags.Error())
		}
		value, _ = value.UnmarkDeep()
		if !value.IsKnown() || value.IsNull() || !value.Type().Equals(cty.Bool) {
			return fmt.Errorf("%s:%d:%s: assert condition must evaluate to a boolean", assert.ConditionSource.File, assert.ConditionSource.Line, assert.ConditionSource.Path)
		}
		if !value.True() {
			return fmt.Errorf("%s:%d:%s: assertion failed: %s", assert.Source.File, assert.Source.Line, assert.Source.Path, assert.Message)
		}
	}
	return nil
}

func hostSpecToCty(host ir.HostSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"name":        cty.StringVal(host.Name),
		"ssh":         sshSpecToCty(host.SSH),
		"state":       stateSpecToCty(host.State),
		"system":      systemSpecToCty(host.System),
		"kernel":      kernelSpecToCty(host.Kernel),
		"packages":    packageSpecToCty(host.Packages),
		"apt":         aptSpecToCty(host.APT),
		"files":       fileSpecToCty(host.Files),
		"secrets":     secretSpecToCty(host.Secrets),
		"directories": directorySpecToCty(host.Directories),
		"groups":      groupSpecToCty(host.Groups),
		"users":       userSpecToCty(host.Users),
		"systemd":     systemdSpecToCty(host.Systemd),
		"services":    serviceSpecToCty(host.Services),
		"nftables":    nftablesSpecToCty(host.Nftables),
	})
}

func sshSpecToCty(spec ir.SSHSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"host":          cty.StringVal(spec.Host),
		"port":          cty.NumberIntVal(int64(spec.Port)),
		"user":          cty.StringVal(spec.User),
		"identity_file": cty.StringVal(spec.IdentityFile),
	})
}

func stateSpecToCty(spec ir.StateSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"path":      cty.StringVal(spec.Path),
		"lock_path": cty.StringVal(spec.LockPath),
	})
}

func systemSpecToCty(spec ir.SystemSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"hostname":     cty.StringVal(spec.Hostname),
		"architecture": cty.StringVal(spec.Architecture),
		"codename":     cty.StringVal(spec.Codename),
		"timezone":     cty.StringVal(spec.Timezone),
		"locale":       cty.StringVal(spec.Locale),
	})
}

func kernelSpecToCty(spec ir.KernelSpec) cty.Value {
	modules := make([]cty.Value, 0, len(spec.Modules))
	for _, module := range spec.Modules {
		modules = append(modules, cty.StringVal(module.Name))
	}

	sysctl := make(map[string]cty.Value, len(spec.Sysctl))
	keys := make([]string, 0, len(spec.Sysctl))
	for key := range spec.Sysctl {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sysctl[key] = cty.StringVal(spec.Sysctl[key].Value)
	}

	return cty.ObjectVal(map[string]cty.Value{
		"modules": stringTuple(modules),
		"sysctl":  objectOrEmpty(sysctl),
	})
}

func packageSpecToCty(spec ir.PackageSpec) cty.Value {
	install := make([]cty.Value, 0, len(spec.Install))
	for _, item := range spec.Install {
		install = append(install, cty.StringVal(item.Name))
	}
	return cty.ObjectVal(map[string]cty.Value{
		"install": stringTuple(install),
	})
}

func aptSpecToCty(spec ir.APTSpec) cty.Value {
	repositories := make(map[string]cty.Value, len(spec.Repositories))
	for _, name := range sortedMapKeys(spec.Repositories) {
		item := spec.Repositories[name]
		repositories[name] = cty.ObjectVal(map[string]cty.Value{
			"name":          cty.StringVal(item.Name),
			"uris":          stringListToCty(item.URIs),
			"suites":        stringListToCty(item.Suites),
			"components":    stringListToCty(item.Components),
			"architectures": stringListToCty(item.Architectures),
			"ensure":        cty.StringVal(item.Ensure),
		})
	}
	sourceFiles := make(map[string]cty.Value, len(spec.SourceFiles))
	for _, label := range sortedMapKeys(spec.SourceFiles) {
		item := spec.SourceFiles[label]
		sourceFiles[label] = cty.ObjectVal(map[string]cty.Value{
			"label":       cty.StringVal(item.Label),
			"path":        cty.StringVal(item.Path),
			"owner":       cty.StringVal(item.Owner),
			"group":       cty.StringVal(item.Group),
			"mode":        cty.StringVal(item.Mode),
			"ensure":      cty.StringVal(item.Ensure),
			"on_destroy":  cty.StringVal(item.OnDestroy),
			"source_path": cty.StringVal(item.SourcePath),
		})
	}
	return cty.ObjectVal(map[string]cty.Value{
		"repositories": objectOrEmpty(repositories),
		"source_files": objectOrEmpty(sourceFiles),
	})
}

func fileSpecToCty(spec ir.FileSpec) cty.Value {
	files := make(map[string]cty.Value, len(spec.Files))
	for _, path := range sortedMapKeys(spec.Files) {
		item := spec.Files[path]
		files[path] = cty.ObjectVal(map[string]cty.Value{
			"path":        cty.StringVal(item.Path),
			"owner":       cty.StringVal(item.Owner),
			"group":       cty.StringVal(item.Group),
			"mode":        cty.StringVal(item.Mode),
			"sensitive":   cty.BoolVal(item.Sensitive),
			"ensure":      cty.StringVal(item.Ensure),
			"source_path": cty.StringVal(item.SourcePath),
		})
	}
	return cty.ObjectVal(map[string]cty.Value{"files": objectOrEmpty(files)})
}

func secretSpecToCty(spec ir.SecretSpec) cty.Value {
	files := make(map[string]cty.Value, len(spec.Files))
	for _, path := range sortedMapKeys(spec.Files) {
		item := spec.Files[path]
		files[path] = cty.ObjectVal(map[string]cty.Value{
			"path":        cty.StringVal(item.Path),
			"owner":       cty.StringVal(item.Owner),
			"group":       cty.StringVal(item.Group),
			"mode":        cty.StringVal(item.Mode),
			"ensure":      cty.StringVal(item.Ensure),
			"source_path": cty.StringVal(item.SourcePath),
		})
	}
	return cty.ObjectVal(map[string]cty.Value{"files": objectOrEmpty(files)})
}

func directorySpecToCty(spec ir.DirectorySpec) cty.Value {
	directories := make(map[string]cty.Value, len(spec.Directories))
	for _, path := range sortedMapKeys(spec.Directories) {
		item := spec.Directories[path]
		directories[path] = cty.ObjectVal(map[string]cty.Value{
			"path":   cty.StringVal(item.Path),
			"owner":  cty.StringVal(item.Owner),
			"group":  cty.StringVal(item.Group),
			"mode":   cty.StringVal(item.Mode),
			"ensure": cty.StringVal(item.Ensure),
		})
	}
	return cty.ObjectVal(map[string]cty.Value{"directories": objectOrEmpty(directories)})
}

func groupSpecToCty(spec ir.GroupSpec) cty.Value {
	groups := make(map[string]cty.Value, len(spec.Groups))
	for _, name := range sortedMapKeys(spec.Groups) {
		item := spec.Groups[name]
		groups[name] = cty.ObjectVal(map[string]cty.Value{
			"name":   cty.StringVal(item.Name),
			"gid":    cty.StringVal(item.GID),
			"system": cty.BoolVal(item.System),
			"ensure": cty.StringVal(item.Ensure),
		})
	}
	return cty.ObjectVal(map[string]cty.Value{"groups": objectOrEmpty(groups)})
}

func userSpecToCty(spec ir.UserSpec) cty.Value {
	users := make(map[string]cty.Value, len(spec.Users))
	for _, name := range sortedMapKeys(spec.Users) {
		item := spec.Users[name]
		users[name] = cty.ObjectVal(map[string]cty.Value{
			"name":                cty.StringVal(item.Name),
			"uid":                 cty.StringVal(item.UID),
			"group":               cty.StringVal(item.PrimaryGroup),
			"groups":              stringListToCty(item.Groups),
			"system":              cty.BoolVal(item.System),
			"home":                cty.StringVal(item.Home),
			"shell":               cty.StringVal(item.Shell),
			"ssh_authorized_keys": stringListToCty(item.SSHAuthorizedKeys),
			"ensure":              cty.StringVal(item.Ensure),
		})
	}
	return cty.ObjectVal(map[string]cty.Value{"users": objectOrEmpty(users)})
}

func systemdSpecToCty(spec ir.SystemdSpec) cty.Value {
	units := make(map[string]cty.Value, len(spec.Units))
	for _, name := range sortedMapKeys(spec.Units) {
		item := spec.Units[name]
		units[name] = cty.ObjectVal(map[string]cty.Value{
			"name":        cty.StringVal(item.Name),
			"path":        cty.StringVal(item.Path),
			"owner":       cty.StringVal(item.Owner),
			"group":       cty.StringVal(item.Group),
			"mode":        cty.StringVal(item.Mode),
			"ensure":      cty.StringVal(item.Ensure),
			"source_path": cty.StringVal(item.SourcePath),
		})
	}
	values := map[string]cty.Value{
		"units":    objectOrEmpty(units),
		"networkd": cty.NullVal(cty.DynamicPseudoType),
	}
	if spec.Networkd != nil {
		values["networkd"] = networkdSpecToCty(*spec.Networkd)
	}
	return cty.ObjectVal(values)
}

func networkdSpecToCty(spec ir.NetworkdSpec) cty.Value {
	netdevs := make(map[string]cty.Value, len(spec.NetDevs))
	for _, label := range sortedMapKeys(spec.NetDevs) {
		item := spec.NetDevs[label]
		peers := make(map[string]cty.Value, len(item.WireGuardPeers))
		for _, peer := range sortedMapKeys(item.WireGuardPeers) {
			peers[peer] = networkdSectionToCty(item.WireGuardPeers[peer])
		}
		netdevs[label] = cty.ObjectVal(map[string]cty.Value{
			"label":          cty.StringVal(item.Label),
			"path":           cty.StringVal(item.Path),
			"netdev":         networkdSectionToCty(item.NetDev),
			"wireguard":      networkdSectionToCty(item.WireGuard),
			"wireguard_peer": objectOrEmpty(peers),
			"owner":          cty.StringVal(item.Owner),
			"group":          cty.StringVal(item.Group),
			"mode":           cty.StringVal(item.Mode),
			"ensure":         cty.StringVal(item.Ensure),
		})
	}
	networks := make(map[string]cty.Value, len(spec.Networks))
	for _, label := range sortedMapKeys(spec.Networks) {
		item := spec.Networks[label]
		networks[label] = cty.ObjectVal(map[string]cty.Value{
			"label":   cty.StringVal(item.Label),
			"path":    cty.StringVal(item.Path),
			"match":   networkdSectionToCty(item.Match),
			"network": networkdSectionToCty(item.Network),
			"owner":   cty.StringVal(item.Owner),
			"group":   cty.StringVal(item.Group),
			"mode":    cty.StringVal(item.Mode),
			"ensure":  cty.StringVal(item.Ensure),
		})
	}
	enable := cty.NullVal(cty.Bool)
	if spec.Enable != nil {
		enable = cty.BoolVal(*spec.Enable)
	}
	return cty.ObjectVal(map[string]cty.Value{
		"enable":  enable,
		"netdev":  objectOrEmpty(netdevs),
		"network": objectOrEmpty(networks),
	})
}

func networkdSectionToCty(section ir.NetworkdSection) cty.Value {
	values := make(map[string]cty.Value, len(section))
	for _, key := range sortedMapKeys(section) {
		items := section[key]
		if len(items) == 1 {
			values[key] = cty.StringVal(items[0])
		} else {
			values[key] = stringListToCty(items)
		}
	}
	return objectOrEmpty(values)
}

func serviceSpecToCty(spec ir.ServiceSpec) cty.Value {
	services := make(map[string]cty.Value, len(spec.Services))
	for _, name := range sortedMapKeys(spec.Services) {
		item := spec.Services[name]
		enabled := cty.NullVal(cty.Bool)
		if item.Enabled != nil {
			enabled = cty.BoolVal(*item.Enabled)
		}
		services[name] = cty.ObjectVal(map[string]cty.Value{
			"name":    cty.StringVal(item.Name),
			"unit":    cty.StringVal(item.Unit),
			"package": cty.StringVal(item.Package),
			"enabled": enabled,
			"state":   cty.StringVal(item.State),
		})
	}
	return cty.ObjectVal(map[string]cty.Value{"services": objectOrEmpty(services)})
}

func nftablesSpecToCty(spec ir.NftablesSpec) cty.Value {
	files := make(map[string]cty.Value, len(spec.Files))
	for _, label := range sortedMapKeys(spec.Files) {
		files[label] = nftablesFileToCty(spec.Files[label])
	}
	values := map[string]cty.Value{
		"enable": cty.BoolVal(false),
		"files":  objectOrEmpty(files),
	}
	if spec.Enable != nil {
		values["enable"] = cty.BoolVal(*spec.Enable)
	}
	if spec.Main != nil {
		values["main"] = nftablesFileToCty(*spec.Main)
	} else {
		values["main"] = cty.NullVal(cty.DynamicPseudoType)
	}
	return cty.ObjectVal(values)
}

func nftablesFileToCty(item ir.NftablesFileSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"label":       cty.StringVal(item.Label),
		"path":        cty.StringVal(item.Path),
		"owner":       cty.StringVal(item.Owner),
		"group":       cty.StringVal(item.Group),
		"mode":        cty.StringVal(item.Mode),
		"sensitive":   cty.BoolVal(item.Sensitive),
		"validate":    cty.BoolVal(item.Validate),
		"activate":    cty.BoolVal(item.Activate),
		"ensure":      cty.StringVal(item.Ensure),
		"source_path": cty.StringVal(item.SourcePath),
	})
}

func stringTuple(values []cty.Value) cty.Value {
	if len(values) == 0 {
		return cty.EmptyTupleVal
	}
	return cty.TupleVal(values)
}

func objectOrEmpty(values map[string]cty.Value) cty.Value {
	if len(values) == 0 {
		return cty.EmptyObjectVal
	}
	return cty.ObjectVal(values)
}

func stringListToCty(values []string) cty.Value {
	out := make([]cty.Value, 0, len(values))
	for _, value := range values {
		out = append(out, cty.StringVal(value))
	}
	return stringTuple(out)
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func containsFunction() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "collection", Type: cty.DynamicPseudoType},
			{Name: "value", Type: cty.DynamicPseudoType},
		},
		Type: function.StaticReturnType(cty.Bool),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			collection, _ := args[0].UnmarkDeep()
			needle, _ := args[1].UnmarkDeep()
			if !collection.IsKnown() || collection.IsNull() {
				return cty.NilVal, fmt.Errorf("contains() collection must be known and non-null")
			}
			ty := collection.Type()
			if !ty.IsTupleType() && !ty.IsListType() && !ty.IsSetType() {
				return cty.NilVal, fmt.Errorf("contains() collection must be a list, tuple, or set")
			}
			it := collection.ElementIterator()
			for it.Next() {
				_, item := it.Element()
				item, _ = item.UnmarkDeep()
				if item.RawEquals(needle) {
					return cty.BoolVal(true), nil
				}
			}
			return cty.BoolVal(false), nil
		},
	})
}
