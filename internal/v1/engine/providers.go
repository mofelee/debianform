package engine

import (
	"context"
	"fmt"

	"github.com/mofelee/debianform/internal/v1/config"
	"github.com/mofelee/debianform/internal/v1/sshx"
	"github.com/mofelee/debianform/internal/v1/state"
)

// provider encapsulates everything a resource type needs: deriving desired
// state from config, planning against the live host, applying a change, and
// destroying a resource that has been removed from configuration using only
// its recorded state. Adding a resource is a matter of implementing this
// interface and registering the value in providers, with no edits to the
// engine's dispatch.
type provider interface {
	Desired(res config.Resource) (Desired, error)
	Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error)
	Apply(ctx context.Context, e *Engine, change Change) error
	Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error
}

// destroyPath removes a single managed file recorded in prior state. It backs
// the destroy of the file-backed resource types.
func destroyPath(ctx context.Context, e *Engine, prior state.ResourceState) error {
	path := priorString(prior, "path")
	if path == "" {
		return nil
	}
	_, err := e.runner.Run(ctx, priorString(prior, "host"), "rm -f -- "+sshx.ShellQuote(path)+"\n")
	return err
}

var providers = map[string]provider{
	"debian_package":        packageProvider{},
	"debian_file":           fileProvider{},
	"debian_networkd_file":  networkdFileProvider{},
	"debian_nftables_file":  nftablesProvider{},
	"debian_directory":      directoryProvider{},
	"debian_service":        serviceProvider{},
	"debian_kernel_module":  kernelModuleProvider{},
	"debian_sysctl":         sysctlProvider{},
	"debian_group":          groupProvider{},
	"debian_user":           userProvider{},
	"debian_authorized_key": authorizedKeyProvider{},
	"debian_hostname":       hostnameProvider{},
	"debian_apt_source":     aptSourceProvider{},
	"debian_apt_repository": aptRepositoryProvider{},
	"debian_release_binary": releaseBinaryProvider{},
	"debian_systemd_unit":   systemdUnitProvider{},
}

func lookupProvider(resType string) (provider, error) {
	p, ok := providers[resType]
	if !ok {
		return nil, fmt.Errorf("unsupported resource type %s", resType)
	}
	return p, nil
}

// desiredFor derives the desired state for a resource via its provider.
func desiredFor(res config.Resource) (Desired, error) {
	p, err := lookupProvider(res.Type)
	if err != nil {
		return Desired{}, err
	}
	return p.Desired(res)
}

// baseDesired holds the defaults shared by every resource type.
func baseDesired(res config.Resource) Desired {
	return Desired{
		Name:   objectName(res),
		Owner:  "root",
		Group:  "root",
		Mode:   "",
		Ensure: "present",
	}
}

type packageProvider struct{}

func (packageProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	d.Ensure = stringAttr(res, "ensure", "present")
	d.Version = stringAttr(res, "version", "")
	d.UpdateCache = boolAttr(res, "update_cache", false)
	return d, nil
}

func (packageProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planPackage(ctx, res, d)
}

func (packageProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applyPackage(ctx, change)
}

type fileProvider struct{}

func (fileProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Path = stringAttr(res, "path", "")
	content, err := contentAttr(res)
	if err != nil {
		return d, err
	}
	d.Content = content
	d.ContentSHA256 = hash(d.Content)
	d.Owner = stringAttr(res, "owner", "root")
	d.Group = stringAttr(res, "group", "root")
	d.Mode = stringAttr(res, "mode", "0644")
	return d, nil
}

func (fileProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planFile(ctx, res, d)
}

func (fileProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applyFile(ctx, change)
}

type networkdFileProvider struct{}

func (networkdFileProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	d.Path = stringAttr(res, "path", "")
	if d.Path == "" {
		d.Path = "/etc/systemd/network/" + d.Name
	}
	content, err := contentAttr(res)
	if err != nil {
		return d, err
	}
	d.Content = content
	d.ContentSHA256 = hash(d.Content)
	d.Owner = stringAttr(res, "owner", "root")
	d.Group = stringAttr(res, "group", "root")
	d.Mode = stringAttr(res, "mode", "0644")
	d.Activate = boolAttr(res, "activate", false)
	return d, nil
}

func (networkdFileProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planFile(ctx, res, d)
}

func (networkdFileProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applyFile(ctx, change)
}

type nftablesProvider struct{}

func (nftablesProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	d.Path = stringAttr(res, "path", "")
	if d.Path == "" {
		if d.Name == "main" {
			d.Path = "/etc/nftables.conf"
		} else {
			d.Path = "/etc/nftables.d/" + d.Name + ".nft"
		}
	}
	content, err := contentAttr(res)
	if err != nil {
		return d, err
	}
	d.Content = content
	d.ContentSHA256 = hash(d.Content)
	d.Owner = stringAttr(res, "owner", "root")
	d.Group = stringAttr(res, "group", "root")
	d.Mode = stringAttr(res, "mode", "0644")
	d.Activate = boolAttr(res, "activate", false)
	d.Validate = boolAttr(res, "validate", true)
	return d, nil
}

func (nftablesProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planFile(ctx, res, d)
}

func (nftablesProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applyNftablesFile(ctx, change)
}

type directoryProvider struct{}

func (directoryProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Path = stringAttr(res, "path", "")
	d.Owner = stringAttr(res, "owner", "")
	d.Group = stringAttr(res, "group", "")
	d.Mode = stringAttr(res, "mode", "")
	d.Ensure = stringAttr(res, "ensure", "present")
	return d, nil
}

func (directoryProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planDirectory(ctx, res, d)
}

func (directoryProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applyDirectory(ctx, change)
}

type serviceProvider struct{}

func (serviceProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	if enabled, ok := res.Attrs["enabled"].(bool); ok {
		d.Enabled = &enabled
	}
	d.ServiceState = stringAttr(res, "state", "")
	d.Package = stringAttr(res, "package", "")
	return d, nil
}

func (serviceProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planService(ctx, res, d)
}

func (serviceProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applyService(ctx, change)
}

type kernelModuleProvider struct{}

func (kernelModuleProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	d.Ensure = stringAttr(res, "ensure", "present")
	d.Persist = boolAttr(res, "persist", true)
	d.Path = stringAttr(res, "path", "")
	if d.Path == "" {
		d.Path = "/etc/modules-load.d/dbf-" + res.Name + ".conf"
	}
	d.Content = d.Name + "\n"
	d.ContentSHA256 = hash(d.Content)
	return d, nil
}

func (kernelModuleProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planKernelModule(ctx, res, d)
}

func (kernelModuleProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applyKernelModule(ctx, change)
}

type sysctlProvider struct{}

func (sysctlProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Key = stringAttr(res, "key", "")
	d.Value = stringAttr(res, "value", "")
	d.Persist = boolAttr(res, "persist", true)
	d.ApplyRuntime = boolAttr(res, "apply", true)
	d.Path = stringAttr(res, "path", "")
	if d.Path == "" {
		d.Path = "/etc/sysctl.d/99-dbf-" + res.Name + ".conf"
	}
	d.Content = d.Key + " = " + d.Value + "\n"
	d.ContentSHA256 = hash(d.Content)
	return d, nil
}

func (sysctlProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planSysctl(ctx, res, d)
}

func (sysctlProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	return e.applySysctl(ctx, change)
}

// Destroy implementations for the file- and command-backed resource types.

func (packageProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	name := priorString(prior, "name")
	if name == "" {
		return nil
	}
	script := "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get remove -y " + sshx.ShellQuote(name) + "\n"
	_, err := e.runner.Run(ctx, priorString(prior, "host"), script)
	return err
}

func (fileProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	return destroyPath(ctx, e, prior)
}

func (networkdFileProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	return destroyPath(ctx, e, prior)
}

func (nftablesProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	return destroyPath(ctx, e, prior)
}

func (sysctlProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	return destroyPath(ctx, e, prior)
}

func (directoryProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	path := priorString(prior, "path")
	if path == "" || path == "/" {
		return nil
	}
	_, err := e.runner.Run(ctx, priorString(prior, "host"), "rm -rf -- "+sshx.ShellQuote(path)+"\n")
	return err
}

func (serviceProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	name := priorString(prior, "name")
	if name == "" {
		return nil
	}
	_, err := e.runner.Run(ctx, priorString(prior, "host"), "systemctl disable --now "+sshx.ShellQuote(name)+" 2>/dev/null || true\n")
	return err
}

func (kernelModuleProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	name := priorString(prior, "name")
	if name == "" {
		return nil
	}
	script := "set -eu\n"
	if path := priorString(prior, "path"); path != "" {
		script += "rm -f -- " + sshx.ShellQuote(path) + "\n"
	}
	script += "modprobe -r " + sshx.ShellQuote(name) + " 2>/dev/null || true\n"
	_, err := e.runner.Run(ctx, priorString(prior, "host"), script)
	return err
}
