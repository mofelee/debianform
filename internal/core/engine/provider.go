package engine

import (
	"context"
	"fmt"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

type NativeProvider struct {
	Runner Runner
}

func NewNativeProvider(runner Runner) NativeProvider {
	return NativeProvider{Runner: runner}
}

func (p NativeProvider) Plan(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	if p.Runner == nil {
		return ProviderPlan{}, fmt.Errorf("provider runner is required")
	}
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "plan inspect",
		Address: node.Address,
		Action:  "inspect",
		Summary: node.Summary,
	})
	switch node.Kind {
	case "system_hostname":
		return p.planSystemHostname(ctx, node, prior)
	case "system_timezone":
		return p.planSystemTimezone(ctx, node, prior)
	case "system_locale":
		return p.planSystemLocale(ctx, node, prior)
	case "file", "secret", "systemd_unit", "nftables_file", "networkd_netdev", "networkd_network":
		return p.planFileLike(ctx, node, prior)
	case "apt_source_file":
		return p.planAPTSourceFile(ctx, node, prior)
	case "apt_signing_key":
		return p.planAPTSigningKey(ctx, node, prior)
	case "component_download":
		return p.planComponentDownload(ctx, node, prior)
	case "component_build":
		return p.planComponentBuild(ctx, node, prior)
	case "component_binary":
		return p.planComponentBinary(ctx, node, prior)
	case "component_file", "component_ca_certificate":
		return p.planComponentFile(ctx, node, prior)
	case "component_archive":
		return p.planComponentArchive(ctx, node, prior)
	case "component_script_output":
		return p.planComponentScriptOutput(ctx, node, prior)
	case "directory":
		return p.planDirectory(ctx, node, prior)
	case "package":
		return p.planPackage(ctx, node, prior)
	case "docker_package_conflicts":
		return p.planDockerPackageConflicts(ctx, node, prior)
	case "kernel_module":
		return p.planKernelModule(ctx, node, prior)
	case "sysctl":
		return p.planSysctl(ctx, node, prior)
	case "group":
		return p.planGroup(ctx, node, prior)
	case "user":
		return p.planUser(ctx, node, prior)
	case "user_group_membership":
		return p.planUserGroupMembership(ctx, node, prior)
	case "ssh_authorized_key":
		return p.planAuthorizedKey(ctx, node, prior)
	case "service":
		return p.planService(ctx, node, prior)
	case "docker_compose_project":
		return p.planDockerComposeProject(ctx, node, prior)
	default:
		return ProviderPlan{}, fmt.Errorf("%s unsupported resource kind %q", node.Address, node.Kind)
	}
}

func (p NativeProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "apply resource",
		Address: step.Address,
		Action:  step.Action,
		Summary: step.Summary,
	})
	switch step.Node.Kind {
	case "system_hostname":
		return p.applySystemHostname(ctx, step)
	case "system_timezone":
		return p.applySystemTimezone(ctx, step)
	case "system_locale":
		return p.applySystemLocale(ctx, step)
	case "file", "secret", "nftables_file", "networkd_netdev", "networkd_network":
		return p.applyFileLike(ctx, step, false)
	case "apt_source_file":
		return p.applyAPTSourceFile(ctx, step)
	case "systemd_unit":
		return p.applyFileLike(ctx, step, true)
	case "apt_signing_key":
		return p.applyAPTSigningKey(ctx, step)
	case "component_download":
		return p.applyComponentDownload(ctx, step)
	case "component_build":
		return p.applyComponentBuild(ctx, step)
	case "component_binary":
		return p.applyComponentBinary(ctx, step)
	case "component_file", "component_ca_certificate":
		return p.applyComponentFile(ctx, step)
	case "component_archive":
		return p.applyComponentArchive(ctx, step)
	case "component_script_output":
		return p.applyComponentScriptOutput(ctx, step)
	case "directory":
		return p.applyDirectory(ctx, step)
	case "package":
		return p.applyPackage(ctx, step)
	case "docker_package_conflicts":
		return p.applyDockerPackageConflicts(ctx, step)
	case "kernel_module":
		return p.applyKernelModule(ctx, step)
	case "sysctl":
		return p.applySysctl(ctx, step)
	case "group":
		return p.applyGroup(ctx, step)
	case "user":
		return p.applyUser(ctx, step)
	case "user_group_membership":
		return p.applyUserGroupMembership(ctx, step)
	case "ssh_authorized_key":
		return p.applyAuthorizedKey(ctx, step)
	case "service":
		return p.applyService(ctx, step)
	case "docker_compose_project":
		return p.applyDockerComposeProject(ctx, step)
	default:
		return nil, fmt.Errorf("%s unsupported resource kind %q", step.Address, step.Node.Kind)
	}
}

func (p NativeProvider) Destroy(ctx context.Context, step Step) error {
	if step.Prior == nil {
		return nil
	}
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "apply resource",
		Address: step.Address,
		Action:  step.Action,
		Summary: step.Summary,
	})
	desired := step.Prior.Desired
	host := step.Host
	if host == "" {
		return fmt.Errorf("%s is missing its target host", step.Address)
	}
	switch step.Prior.Kind {
	case "system_hostname", "system_timezone", "system_locale":
		return nil
	case "apt_source_file":
		return p.destroyAPTSourceFile(ctx, step)
	case "component_build":
		return p.removePath(ctx, host, stringMapValue(desired, "output_path"), false)
	case "file", "secret", "nftables_file", "networkd_netdev", "networkd_network", "apt_signing_key", "component_download", "component_binary", "component_file", "component_ca_certificate":
		return p.removePath(ctx, host, stringMapValue(desired, "path"), false)
	case "component_script_output":
		return nil
	case "component_archive":
		return p.removeDirectory(ctx, host, stringMapValue(desired, "path"))
	case "systemd_unit":
		return p.removePath(ctx, host, stringMapValue(desired, "path"), true)
	case "directory":
		path := stringMapValue(desired, "path")
		if path == "" || path == "/" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "rm -rf -- "+shellQuote(path)+"\n")
		return err
	case "package":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get remove -y "+shellQuote(name)+"\n")
		return err
	case "docker_package_conflicts":
		return nil
	case "kernel_module":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		script := "set -eu\nrm -f -- " + shellQuote(modulePath(name)) + "\nmodprobe -r " + shellQuote(name) + " 2>/dev/null || true\n"
		_, err := p.Runner.Run(ctx, host, script)
		return err
	case "sysctl":
		key := stringMapValue(desired, "key")
		if key == "" {
			return nil
		}
		return p.removePath(ctx, host, sysctlPath(key), false)
	case "group":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "if getent group "+shellQuote(name)+" >/dev/null; then groupdel "+shellQuote(name)+"; fi\n")
		return err
	case "user":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "if getent passwd "+shellQuote(name)+" >/dev/null; then userdel "+shellQuote(name)+"; fi\n")
		return err
	case "user_group_membership":
		user := stringMapValue(desired, "user")
		group := stringMapValue(desired, "group")
		if user == "" || group == "" {
			return nil
		}
		node := graph.Node{Address: step.Address, Host: host, Desired: map[string]any{"user": user, "group": group, "ensure": "absent"}}
		_, err := p.applyUserGroupMembership(ctx, Step{Node: node, Action: ActionDelete})
		return err
	case "ssh_authorized_key":
		user := stringMapValue(desired, "user")
		key := stringMapValue(desired, "key")
		if user == "" || key == "" {
			return nil
		}
		node := graph.Node{Host: host, Desired: map[string]any{"user": user, "key": key, "ensure": "absent"}}
		_, err := p.applyAuthorizedKey(ctx, Step{Node: node})
		return err
	case "service":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "systemctl disable --now "+shellQuote(name)+" 2>/dev/null || true\n")
		return err
	case "docker_compose_project":
		node := graph.Node{Address: step.Address, Host: host, Desired: cloneMap(desired)}
		node.Desired["state"] = "absent"
		command, err := dockerComposeProjectCommand(node)
		if err != nil {
			return err
		}
		_, err = p.Runner.Run(ctx, host, command)
		return err
	default:
		return fmt.Errorf("%s unsupported prior kind %q", step.Address, step.Prior.Kind)
	}
}
