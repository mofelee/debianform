package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

type Config struct {
	Files     []string
	Hosts     map[string]Host
	Locals    map[string]any
	State     StateConfig
	Handlers  []Handler
	Resources []Resource
}

type Host struct {
	Name          string
	Address       string
	SSHConfigHost string
	Port          string
	IdentityFile  string
}

type StateConfig struct {
	Host     string
	Path     string
	LockPath string
}

type Handler struct {
	Name    string
	Address string
	Host    string
	Command string
	Order   int
}

type Resource struct {
	Type      string
	Name      string
	Key       *string
	Address   string
	Host      string
	Attrs     map[string]any
	DependsOn []string
	Notify    []string
	Order     int
}

func Load(files []string) (*Config, error) {
	cfg := &Config{
		Files:  files,
		Hosts:  map[string]Host{},
		Locals: map[string]any{},
	}
	var blocks []Block
	for _, file := range files {
		parsed, err := ParseFile(file)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, parsed...)
	}

	for _, block := range blocks {
		if block.Type != "locals" {
			continue
		}
		if block.Label != "" {
			return nil, fmt.Errorf("%s:%d: locals block must not have a label", block.File, block.Line)
		}
		moduleDir := filepath.Dir(block.File)
		ctx := EvalContext{PathModule: moduleDir, Locals: cfg.Locals}
		for name, expr := range block.Attrs {
			value, err := Eval(expr, ctx)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: local.%s: %w", block.File, block.Line, name, err)
			}
			cfg.Locals[name] = value
		}
	}

	order := 0
	for _, block := range blocks {
		moduleDir := filepath.Dir(block.File)
		switch block.Type {
		case "locals":
			continue
		case "host":
			host, err := loadHost(block, moduleDir, cfg.Locals)
			if err != nil {
				return nil, err
			}
			cfg.Hosts[host.Name] = host
		case "state":
			if block.Label != "ssh" {
				return nil, fmt.Errorf("%s:%d: only state \"ssh\" is supported", block.File, block.Line)
			}
			st, err := loadState(block, moduleDir, cfg.Locals)
			if err != nil {
				return nil, err
			}
			cfg.State = st
		case "handler":
			handler, err := loadHandler(block, moduleDir, cfg.Locals, order)
			if err != nil {
				return nil, err
			}
			cfg.Handlers = append(cfg.Handlers, handler)
			order++
		default:
			resources, err := expandResource(block, moduleDir, cfg.Locals, order)
			if err != nil {
				return nil, err
			}
			cfg.Resources = append(cfg.Resources, resources...)
			order += len(resources)
		}
	}

	if cfg.State.Host == "" || cfg.State.Path == "" {
		return nil, fmt.Errorf("state \"ssh\" block with host and path is required")
	}
	if cfg.State.LockPath == "" {
		cfg.State.LockPath = cfg.State.Path + ".lock"
	}

	sortResources(cfg.Resources)
	for i := range cfg.Resources {
		if err := validateResource(cfg.Resources[i]); err != nil {
			return nil, err
		}
	}
	if err := validateHandlers(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadHost(block Block, moduleDir string, locals map[string]any) (Host, error) {
	if block.Label == "" {
		return Host{}, fmt.Errorf("%s:%d: host block requires a label", block.File, block.Line)
	}
	ctx := EvalContext{PathModule: moduleDir, Locals: locals}
	host := Host{Name: block.Label}
	var err error
	host.Address, err = optionalString(block, ctx, "address")
	if err != nil {
		return Host{}, err
	}
	host.SSHConfigHost, err = optionalString(block, ctx, "ssh_config_host")
	if err != nil {
		return Host{}, err
	}
	host.Port, err = optionalString(block, ctx, "port")
	if err != nil {
		return Host{}, err
	}
	host.IdentityFile, err = optionalString(block, ctx, "identity_file")
	if err != nil {
		return Host{}, err
	}
	return host, nil
}

func loadState(block Block, moduleDir string, locals map[string]any) (StateConfig, error) {
	ctx := EvalContext{PathModule: moduleDir, Locals: locals}
	var st StateConfig
	var err error
	st.Host, err = requiredString(block, ctx, "host")
	if err != nil {
		return st, err
	}
	st.Path, err = requiredString(block, ctx, "path")
	if err != nil {
		return st, err
	}
	st.LockPath, err = optionalString(block, ctx, "lock_path")
	if err != nil {
		return st, err
	}
	return st, nil
}

func loadHandler(block Block, moduleDir string, locals map[string]any, order int) (Handler, error) {
	if block.Label == "" {
		return Handler{}, fmt.Errorf("%s:%d: handler block requires a label", block.File, block.Line)
	}
	if !isLocalName(block.Label) {
		return Handler{}, fmt.Errorf("%s:%d: handler name %q must use letters, digits, and underscores", block.File, block.Line, block.Label)
	}
	ctx := EvalContext{PathModule: moduleDir, Locals: locals}
	host, err := requiredString(block, ctx, "host")
	if err != nil {
		return Handler{}, err
	}
	command, err := requiredString(block, ctx, "command")
	if err != nil {
		return Handler{}, err
	}
	return Handler{
		Name:    block.Label,
		Address: "handler." + block.Label,
		Host:    host,
		Command: command,
		Order:   order,
	}, nil
}

func expandResource(block Block, moduleDir string, locals map[string]any, order int) ([]Resource, error) {
	if block.Label == "" {
		return nil, fmt.Errorf("%s:%d: resource %s requires a label", block.File, block.Line, block.Type)
	}
	if !isLocalName(block.Label) {
		return nil, fmt.Errorf("%s:%d: resource name %q must use letters, digits, and underscores", block.File, block.Line, block.Label)
	}

	if forEachExpr, ok := block.Attrs["for_each"]; ok {
		values, err := evalForEach(forEachExpr, moduleDir, locals)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: invalid for_each: %w", block.File, block.Line, err)
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]Resource, 0, len(keys))
		for i, key := range keys {
			ctx := EvalContext{PathModule: moduleDir, Locals: locals, EachKey: key, EachValue: values[key], HasEach: true}
			res, err := buildResource(block, ctx, key, order+i)
			if err != nil {
				return nil, err
			}
			out = append(out, res)
		}
		return out, nil
	}

	ctx := EvalContext{PathModule: moduleDir, Locals: locals}
	res, err := buildResource(block, ctx, "", order)
	if err != nil {
		return nil, err
	}
	return []Resource{res}, nil
}

func buildResource(block Block, ctx EvalContext, key string, order int) (Resource, error) {
	attrs := map[string]any{}
	for name, expr := range block.Attrs {
		if name == "for_each" || name == "depends_on" || name == "notify" {
			continue
		}
		value, err := Eval(expr, ctx)
		if err != nil {
			return Resource{}, fmt.Errorf("%s:%d: %s.%s: %s: %w", block.File, block.Line, block.Type, block.Label, name, err)
		}
		attrs[name] = value
	}

	host, ok := attrs["host"].(string)
	if !ok || host == "" {
		return Resource{}, fmt.Errorf("%s:%d: %s.%s requires host", block.File, block.Line, block.Type, block.Label)
	}
	delete(attrs, "host")

	deps, err := refs(block.Attrs["depends_on"])
	if err != nil {
		return Resource{}, fmt.Errorf("%s:%d: %s.%s depends_on: %w", block.File, block.Line, block.Type, block.Label, err)
	}

	notify, err := refs(block.Attrs["notify"])
	if err != nil {
		return Resource{}, fmt.Errorf("%s:%d: %s.%s notify: %w", block.File, block.Line, block.Type, block.Label, err)
	}

	var keyPtr *string
	address := block.Type + "." + block.Label
	if ctx.HasEach {
		keyCopy := key
		keyPtr = &keyCopy
		address += "[" + strconv.Quote(key) + "]"
	}

	return Resource{
		Type:      block.Type,
		Name:      block.Label,
		Key:       keyPtr,
		Address:   address,
		Host:      host,
		Attrs:     attrs,
		DependsOn: deps,
		Notify:    notify,
		Order:     order,
	}, nil
}

func evalForEach(expr Expr, moduleDir string, locals map[string]any) (map[string]any, error) {
	value, err := Eval(expr, EvalContext{PathModule: moduleDir, Locals: locals})
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case map[string]any:
		return v, nil
	case []any:
		out := make(map[string]any, len(v))
		for _, item := range v {
			key, ok := item.(string)
			if !ok || key == "" {
				return nil, fmt.Errorf("list for_each entries must be non-empty strings")
			}
			if _, exists := out[key]; exists {
				return nil, fmt.Errorf("duplicate for_each key %q", key)
			}
			out[key] = key
		}
		return out, nil
	default:
		return nil, fmt.Errorf("for_each must be a map or list of strings")
	}
}

func requiredString(block Block, ctx EvalContext, name string) (string, error) {
	value, ok := block.Attrs[name]
	if !ok {
		return "", fmt.Errorf("%s:%d: missing required field %q", block.File, block.Line, name)
	}
	evaluated, err := Eval(value, ctx)
	if err != nil {
		return "", err
	}
	s, ok := evaluated.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%s:%d: field %q must be a non-empty string", block.File, block.Line, name)
	}
	return s, nil
}

func optionalString(block Block, ctx EvalContext, name string) (string, error) {
	value, ok := block.Attrs[name]
	if !ok {
		return "", nil
	}
	evaluated, err := Eval(value, ctx)
	if err != nil {
		return "", err
	}
	switch v := evaluated.(type) {
	case string:
		return v, nil
	case Number:
		return string(v), nil
	default:
		return "", fmt.Errorf("%s:%d: field %q must be a string", block.File, block.Line, name)
	}
}

func refs(expr Expr) ([]string, error) {
	if expr == nil {
		return nil, nil
	}
	items, diags := hcl.ExprList(expr)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		traversal, diags := hcl.AbsTraversalForExpr(item)
		if diags.HasErrors() {
			return nil, fmt.Errorf("%s", diags.Error())
		}
		address, err := traversalAddress(traversal)
		if err != nil {
			return nil, err
		}
		out = append(out, address)
	}
	return out, nil
}

func traversalAddress(traversal hcl.Traversal) (string, error) {
	if len(traversal) < 2 {
		return "", fmt.Errorf("entries must be resource addresses")
	}
	root, ok := traversal[0].(hcl.TraverseRoot)
	if !ok || root.Name == "" {
		return "", fmt.Errorf("entries must be resource addresses")
	}
	name, ok := traversal[1].(hcl.TraverseAttr)
	if !ok || name.Name == "" {
		return "", fmt.Errorf("entries must be resource addresses")
	}

	address := root.Name + "." + name.Name
	for _, part := range traversal[2:] {
		switch step := part.(type) {
		case hcl.TraverseIndex:
			key := step.Key
			key, _ = key.UnmarkDeep()
			if key.IsNull() || !key.IsKnown() || !key.Type().Equals(cty.String) {
				return "", fmt.Errorf("resource instance keys must be strings")
			}
			address += "[" + strconv.Quote(key.AsString()) + "]"
		default:
			return "", fmt.Errorf("resource addresses may only use string instance keys")
		}
	}

	return address, nil
}

func validateResource(res Resource) error {
	switch res.Type {
	case "debian_package":
		if err := validateEnum(res, "ensure", []string{"present", "absent"}, "present"); err != nil {
			return err
		}
	case "debian_file":
		if _, ok := stringAttr(res, "path"); !ok {
			return fmt.Errorf("%s requires path", res.Address)
		}
		if !hasOneOf(res, "content", "source") {
			return fmt.Errorf("%s requires content or source", res.Address)
		}
	case "debian_directory":
		if _, ok := stringAttr(res, "path"); !ok {
			return fmt.Errorf("%s requires path", res.Address)
		}
		if err := validateEnum(res, "ensure", []string{"present", "absent"}, "present"); err != nil {
			return err
		}
	case "debian_service":
		if err := validateEnum(res, "state", []string{"running", "stopped", "restarted", "reloaded", ""}, ""); err != nil {
			return err
		}
	case "debian_networkd_file":
		if _, hasPath := stringAttr(res, "path"); !hasPath {
			name := resourceObjectName(res)
			if name == "" {
				return fmt.Errorf("%s requires name or path", res.Address)
			}
		}
		if !hasOneOf(res, "content", "source") {
			return fmt.Errorf("%s requires content or source", res.Address)
		}
	case "debian_nftables_file":
		if !hasOneOf(res, "content", "source") {
			return fmt.Errorf("%s requires content or source", res.Address)
		}
	case "debian_kernel_module":
		if name := resourceObjectName(res); name == "" {
			return fmt.Errorf("%s requires name", res.Address)
		}
		if err := validateEnum(res, "ensure", []string{"present", "absent"}, "present"); err != nil {
			return err
		}
	case "debian_sysctl":
		if _, ok := stringAttr(res, "key"); !ok {
			return fmt.Errorf("%s requires key", res.Address)
		}
		if _, ok := stringAttr(res, "value"); !ok {
			return fmt.Errorf("%s requires value", res.Address)
		}
	case "debian_group":
		if name := resourceObjectName(res); name == "" {
			return fmt.Errorf("%s requires name", res.Address)
		}
		if err := validateEnum(res, "ensure", []string{"present", "absent"}, "present"); err != nil {
			return err
		}
	case "debian_user":
		if name := resourceObjectName(res); name == "" {
			return fmt.Errorf("%s requires name", res.Address)
		}
		if err := validateEnum(res, "ensure", []string{"present", "absent"}, "present"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported resource type %s", res.Type)
	}
	return nil
}

func validateHandlers(cfg *Config) error {
	handlers := map[string]struct{}{}
	for _, handler := range cfg.Handlers {
		if _, exists := handlers[handler.Address]; exists {
			return fmt.Errorf("duplicate handler %s", handler.Address)
		}
		handlers[handler.Address] = struct{}{}
	}
	for _, res := range cfg.Resources {
		for _, target := range res.Notify {
			if _, ok := handlers[target]; !ok {
				return fmt.Errorf("%s notifies unknown handler %s", res.Address, target)
			}
		}
	}
	return nil
}

func validateEnum(res Resource, field string, allowed []string, def string) error {
	value, ok := stringAttr(res, field)
	if !ok {
		if def != "" {
			res.Attrs[field] = def
		}
		return nil
	}
	for _, item := range allowed {
		if value == item {
			return nil
		}
	}
	return fmt.Errorf("%s field %s has invalid value %q", res.Address, field, value)
}

func stringAttr(res Resource, name string) (string, bool) {
	value, ok := res.Attrs[name]
	if !ok {
		return "", false
	}
	switch v := value.(type) {
	case string:
		return v, v != ""
	case Number:
		return string(v), true
	default:
		return "", false
	}
}

func hasOneOf(res Resource, a, b string) bool {
	_, hasA := stringAttr(res, a)
	_, hasB := stringAttr(res, b)
	return hasA != hasB
}

func resourceObjectName(res Resource) string {
	if name, ok := stringAttr(res, "name"); ok {
		return name
	}
	if res.Key != nil {
		return *res.Key
	}
	return res.Name
}

func isLocalName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func sortResources(resources []Resource) {
	sort.SliceStable(resources, func(i, j int) bool {
		return resources[i].Order < resources[j].Order
	})
}

func String(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case Number:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func Bool(value any) bool {
	v, _ := value.(bool)
	return v
}

func StringList(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, String(item))
	}
	return out
}

func PathModule(files []string) string {
	if len(files) == 0 {
		return "."
	}
	return strings.TrimSuffix(filepath.Dir(files[0]), string(filepath.Separator))
}
