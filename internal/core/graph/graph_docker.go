package graph

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

const (
	dockerOfficialRepositoryName = "docker-official"
	dockerOfficialKeyPath        = "/etc/apt/keyrings/docker.asc"
)

var dockerOfficialPackages = []string{
	"docker-ce",
	"docker-ce-cli",
	"containerd.io",
	"docker-buildx-plugin",
	"docker-compose-plugin",
}

var dockerConflictPackages = []string{
	"docker.io",
	"docker-doc",
	"docker-compose",
	"podman-docker",
	"containerd",
	"runc",
}

type dockerEngineGraph struct {
	Nodes                    []Node
	Operations               []Operation
	RepositoryTriggers       []string
	PackageAddresses         []string
	ServiceAddress           string
	DaemonFileAddress        string
	ComposeValidateAddresses map[string]string
}

func dockerEngineNodes(host ir.HostSpec, repositoryAddresses map[string]string, groupAddresses map[string]string, userAddresses map[string]string) (dockerEngineGraph, error) {
	docker := host.Docker
	if docker == nil || !docker.Enable {
		return dockerEngineGraph{}, nil
	}
	installPackages := true
	if docker.Package.Channel != "stable" {
		return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: docker package channel %q is not implemented in this loop", docker.Package.SourceRef.File, docker.Package.SourceRef.Line, docker.Package.SourceRef.Path, docker.Package.Channel)
	}
	if docker.Package.Version != nil {
		return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: docker package version pinning is not implemented in this loop", docker.Package.SourceRef.File, docker.Package.SourceRef.Line, docker.Package.SourceRef.Path)
	}

	switch docker.Package.Source {
	case "official":
	case "none":
		installPackages = false
	case "custom":
		installPackages = false
	default:
		return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: docker package source %q is not supported", docker.Package.SourceRef.File, docker.Package.SourceRef.Line, docker.Package.SourceRef.Path, docker.Package.Source)
	}

	out := dockerEngineGraph{}
	repositoryAddress := ""

	switch docker.Package.Source {
	case "official":
		distribution := host.PlatformDistribution()
		if distribution == "ubuntu" && host.PlatformVersion() == "" {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s.platform.version: host %q must declare platform.version to compile Ubuntu docker official repository", host.Source.File, host.Source.Line, host.Source.Path, host.Name)
		}
		architecture := host.PlatformArchitecture()
		if architecture == "" {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s.platform.architecture: host %q must declare platform.architecture to compile docker official repository", host.Source.File, host.Source.Line, host.Source.Path, host.Name)
		}
		codename := host.PlatformCodename()
		if codename == "" {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s.platform.codename: host %q must declare platform.codename to compile docker official repository", host.Source.File, host.Source.Line, host.Source.Path, host.Name)
		}
		if distribution != "" && distribution != "debian" {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s.platform.distribution: docker official repository for platform.distribution %q is not implemented", host.Source.File, host.Source.Line, host.Source.Path, distribution)
		}
		if _, exists := host.APT.Repositories[dockerOfficialRepositoryName]; exists {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: apt repository %q conflicts with docker official repository", host.Source.File, host.Source.Line, host.Source.Path, dockerOfficialRepositoryName)
		}
		if _, exists := repositoryAddresses[dockerOfficialRepositoryName]; exists {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: apt repository %q conflicts with docker official repository", host.Source.File, host.Source.Line, host.Source.Path, dockerOfficialRepositoryName)
		}

		keyAddress := fmt.Sprintf("host.%s.docker.apt.signing_key[%s]", host.Name, strconv.Quote(dockerOfficialRepositoryName))
		repositoryAddress = fmt.Sprintf("host.%s.docker.apt.repository[%s]", host.Name, strconv.Quote(dockerOfficialRepositoryName))
		sourcePath := aptRepositorySourcePath(dockerOfficialRepositoryName)
		repositoryURL := docker.Package.RepositoryURL
		if repositoryURL == "" {
			repositoryURL = ir.DockerOfficialRepositoryURL
		}
		gpgURL := docker.Package.GPGURL
		if gpgURL == "" {
			gpgURL = ir.DockerOfficialGPGURL
		}
		gpgSHA256 := docker.Package.GPGSHA256
		if gpgSHA256 == "" && gpgURL == ir.DockerOfficialGPGURL {
			gpgSHA256 = ir.DockerOfficialGPGSHA256
		}

		keyDesired := map[string]any{
			"name":   dockerOfficialRepositoryName,
			"path":   dockerOfficialKeyPath,
			"owner":  "root",
			"group":  "root",
			"mode":   "0644",
			"ensure": "present",
			"url":    gpgURL,
		}
		if gpgSHA256 != "" {
			keyDesired["sha256"] = gpgSHA256
		}
		repositorySpec := ir.APTRepositorySpec{
			Name:          dockerOfficialRepositoryName,
			URIs:          []string{repositoryURL},
			Suites:        []string{codename},
			Components:    []string{docker.Package.Channel},
			Architectures: []string{architecture},
			Ensure:        "present",
			SigningKey: &ir.APTSigningKeySpec{
				Path: dockerOfficialKeyPath,
			},
		}
		repositoryDesired := map[string]any{
			"name":    dockerOfficialRepositoryName,
			"path":    sourcePath,
			"owner":   "root",
			"group":   "root",
			"mode":    "0644",
			"ensure":  "present",
			"content": aptRepositorySourceContent(repositorySpec),
		}
		out.Nodes = append(out.Nodes, Node{
			Host:            host.Name,
			Address:         keyAddress,
			Kind:            "apt_signing_key",
			Summary:         "manage docker official apt signing key",
			Source:          docker.Package.SourceRef,
			Desired:         keyDesired,
			ProviderType:    "apt_signing_key",
			ProviderAddress: "apt_signing_key." + providerName(host.Name, dockerOfficialRepositoryName),
			ProviderPayload: keyDesired,
		}, Node{
			Host:            host.Name,
			Address:         repositoryAddress,
			Kind:            "file",
			Summary:         "manage docker official apt repository",
			Source:          docker.Package.SourceRef,
			Desired:         repositoryDesired,
			DependsOn:       []string{keyAddress},
			ProviderType:    "file",
			ProviderAddress: "file." + providerName(host.Name, sourcePath),
			ProviderPayload: repositoryDesired,
		})
		out.RepositoryTriggers = append(out.RepositoryTriggers, keyAddress, repositoryAddress)

		conflictAddress := fmt.Sprintf("host.%s.docker.package_conflicts", host.Name)
		conflictDesired := map[string]any{
			"packages":         append([]string(nil), dockerConflictPackages...),
			"remove_conflicts": docker.Package.RemoveConflicts,
			"ensure":           "absent",
		}
		out.Nodes = append(out.Nodes, Node{
			Host:            host.Name,
			Address:         conflictAddress,
			Kind:            "docker_package_conflicts",
			Summary:         "remove docker conflict packages",
			Source:          docker.Package.SourceRef,
			Desired:         conflictDesired,
			ProviderType:    "docker_package_conflicts",
			ProviderAddress: "docker_package_conflicts." + providerName(host.Name, "docker"),
			ProviderPayload: conflictDesired,
		})

		for _, name := range dockerOfficialPackages {
			address := fmt.Sprintf("host.%s.docker.package[%s]", host.Name, strconv.Quote(name))
			desired := map[string]any{
				"name":         name,
				"ensure":       "present",
				"repositories": []string{dockerOfficialRepositoryName},
			}
			out.Nodes = append(out.Nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "package",
				Summary:         "install docker package " + name,
				Source:          docker.Package.SourceRef,
				Desired:         desired,
				DependsOn:       []string{repositoryAddress, conflictAddress},
				ProviderType:    "package",
				ProviderAddress: "package." + providerName(host.Name, "docker", name),
				ProviderPayload: desired,
			})
			out.PackageAddresses = append(out.PackageAddresses, address)
		}
	}

	if len(docker.Users) > 0 {
		out.Nodes = append(out.Nodes, dockerUserMembershipNodes(host, groupAddresses, userAddresses)...)
	}

	if docker.Daemon != nil {
		fileAddress, fileNode, restartOperation, err := dockerDaemonGraph(host)
		if err != nil {
			return dockerEngineGraph{}, err
		}
		if len(out.PackageAddresses) > 0 {
			fileNode.DependsOn = dedupeStrings(append(fileNode.DependsOn, out.PackageAddresses...))
		}
		out.Nodes = append(out.Nodes, fileNode)
		out.Operations = append(out.Operations, restartOperation)
		out.DaemonFileAddress = fileAddress
	}

	serviceAddress := fmt.Sprintf("host.%s.docker.service[%s]", host.Name, strconv.Quote("docker"))
	serviceDesired := map[string]any{
		"name":    "docker",
		"unit":    "docker.service",
		"enabled": docker.Service.Enable,
		"state":   docker.Service.State,
	}
	out.Nodes = append(out.Nodes, Node{
		Host:            host.Name,
		Address:         serviceAddress,
		Kind:            "service",
		Summary:         "manage docker service",
		Source:          docker.Service.SourceRef,
		Desired:         serviceDesired,
		ProviderType:    "service",
		ProviderAddress: "service." + providerName(host.Name, "docker"),
		ProviderPayload: serviceDesired,
	})
	out.ServiceAddress = serviceAddress
	if len(out.Operations) > 0 {
		for i := range out.Operations {
			if out.Operations[i].Address == "host."+host.Name+".docker.daemon.restart" {
				out.Operations[i].DependsOn = dedupeStrings(append(out.Operations[i].DependsOn, serviceAddress))
			}
		}
	}

	composeServiceAddress := serviceAddress
	if !installPackages {
		composeServiceAddress = ""
	}
	composeNodes, composeOperations, composeValidates, err := dockerComposeGraph(host, out.PackageAddresses, serviceAddress, composeServiceAddress)
	if err != nil {
		return dockerEngineGraph{}, err
	}
	out.Nodes = append(out.Nodes, composeNodes...)
	out.Operations = append(out.Operations, composeOperations...)
	out.ComposeValidateAddresses = composeValidates

	return out, nil
}

func dockerUserMembershipNodes(host ir.HostSpec, groupAddresses map[string]string, userAddresses map[string]string) []Node {
	docker := host.Docker
	if docker == nil || len(docker.Users) == 0 {
		return nil
	}
	nodes := []Node{}
	groupAddress, groupDeclared := groupAddresses["docker"]
	if !groupDeclared {
		groupAddress = fmt.Sprintf("host.%s.docker.group[%s]", host.Name, strconv.Quote("docker"))
		desired := map[string]any{
			"name":   "docker",
			"gid":    "",
			"system": false,
			"ensure": "present",
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         groupAddress,
			Kind:            "group",
			Summary:         "create docker group",
			Source:          docker.Source,
			Desired:         desired,
			ProviderType:    "group",
			ProviderAddress: "group." + providerName(host.Name, "docker"),
			ProviderPayload: desired,
		})
	}
	for _, user := range dedupeStrings(docker.Users) {
		address := fmt.Sprintf("host.%s.docker.user_group_membership[%s]", host.Name, strconv.Quote(user+":docker"))
		deps := []string{groupAddress}
		if userAddress, ok := userAddresses[user]; ok {
			deps = append(deps, userAddress)
		}
		desired := map[string]any{
			"user":   user,
			"group":  "docker",
			"ensure": "present",
			"note":   "user must log out and back in for docker group membership to affect existing sessions",
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "user_group_membership",
			Summary:         "add user " + user + " to docker group",
			Source:          docker.Source,
			Desired:         desired,
			DependsOn:       dedupeStrings(deps),
			ProviderType:    "user_group_membership",
			ProviderAddress: "user_group_membership." + providerName(host.Name, user, "docker"),
			ProviderPayload: desired,
		})
	}
	return nodes
}

func dockerDaemonGraph(host ir.HostSpec) (string, Node, Operation, error) {
	daemon := host.Docker.Daemon
	if daemon == nil {
		return "", Node{}, Operation{}, nil
	}
	content, err := deterministicJSON(daemon.Settings)
	if err != nil {
		return "", Node{}, Operation{}, err
	}
	address := fmt.Sprintf("host.%s.docker.daemon.file[%s]", host.Name, strconv.Quote("/etc/docker/daemon.json"))
	desired := map[string]any{
		"path":    "/etc/docker/daemon.json",
		"content": content,
		"owner":   "root",
		"group":   "root",
		"mode":    "0644",
		"ensure":  "present",
		"summary": daemon.Summary,
	}
	node := Node{
		Host:            host.Name,
		Address:         address,
		Kind:            "file",
		Summary:         "manage docker daemon configuration",
		Source:          daemon.Source,
		Desired:         desired,
		ProviderType:    "file",
		ProviderAddress: "file." + providerName(host.Name, "/etc/docker/daemon.json"),
		ProviderPayload: desired,
	}
	operation := Operation{
		Address:        "host." + host.Name + ".docker.daemon.restart",
		Action:         "run",
		Summary:        "restart docker service",
		DependsOn:      []string{address},
		TriggeredBy:    []string{address},
		CommandPreview: "systemctl restart docker.service",
		Source:         daemon.Source,
	}
	return address, node, operation, nil
}

func dockerComposeGraph(host ir.HostSpec, packageAddresses []string, fileServiceAddress string, projectServiceAddress string) ([]Node, []Operation, map[string]string, error) {
	if host.Docker == nil || len(host.Docker.Composes) == 0 {
		return nil, nil, map[string]string{}, nil
	}
	validateAddresses := map[string]string{}
	var nodes []Node
	var operations []Operation
	for _, name := range sortedKeys(host.Docker.Composes) {
		compose := host.Docker.Composes[name]
		if !compose.Enable {
			continue
		}
		prefix := fmt.Sprintf("host.%s.docker.compose[%s]", host.Name, strconv.Quote(name))
		directoryAddress := prefix + ".directory"
		directoryDesired := map[string]any{
			"path":   compose.Directory,
			"owner":  "root",
			"group":  "root",
			"mode":   "0755",
			"ensure": "present",
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         directoryAddress,
			Kind:            "directory",
			Summary:         "create docker compose directory " + compose.Directory,
			Source:          compose.Source,
			Desired:         directoryDesired,
			ProviderType:    "directory",
			ProviderAddress: "directory." + providerName(host.Name, "docker", "compose", name, compose.Directory),
			ProviderPayload: directoryDesired,
		})

		fileAddresses := []string{}
		engineDeps := []string{}
		if len(packageAddresses) > 0 {
			engineDeps = append(engineDeps, packageAddresses...)
		}
		if fileServiceAddress != "" {
			engineDeps = append(engineDeps, fileServiceAddress)
		}
		fileDeps := dedupeStrings(append([]string{directoryAddress}, engineDeps...))

		if compose.File == nil {
			return nil, nil, nil, fmt.Errorf("%s:%d:%s.file: docker compose file block is required", compose.Source.File, compose.Source.Line, compose.Source.Path)
		}
		fileAddress, fileNode := dockerComposeFileNode(host.Name, prefix+".file", name, *compose.File, "manage docker compose file", fileDeps)
		nodes = append(nodes, fileNode)
		fileAddresses = append(fileAddresses, fileAddress)

		for _, label := range sortedKeys(compose.EnvFiles) {
			envFile := compose.EnvFiles[label]
			envAddress, envNode := dockerComposeFileNode(host.Name, fmt.Sprintf("%s.env_file[%s]", prefix, strconv.Quote(label)), name, envFile, "manage docker compose env file "+label, fileDeps)
			nodes = append(nodes, envNode)
			fileAddresses = append(fileAddresses, envAddress)
		}

		validateAddress := prefix + ".validate"
		validateAddresses[name] = validateAddress
		operations = append(operations, Operation{
			Address:        validateAddress,
			Action:         "run",
			Summary:        "validate docker compose project " + compose.Project,
			DependsOn:      dedupeStrings(fileAddresses),
			TriggeredBy:    dedupeStrings(fileAddresses),
			CommandPreview: dockerComposeConfigCommand(compose),
			Source:         compose.Source,
		})

		unitAddress := ""
		daemonReloadAddress := ""
		serviceAddress := ""
		if compose.Service.Enable {
			unitAddress = prefix + ".systemd_unit"
			daemonReloadAddress = prefix + ".daemon_reload"
			serviceAddress = prefix + ".service"
			unitName := composeSystemdUnitName(compose)
			unitDesired := dockerComposeSystemdUnitDesired(compose, unitName)
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         unitAddress,
				Kind:            "systemd_unit",
				Summary:         "create docker compose systemd unit " + unitName,
				Source:          compose.Source,
				Desired:         unitDesired,
				DependsOn:       []string{validateAddress},
				ProviderType:    "systemd_unit",
				ProviderAddress: "systemd_unit." + providerName(host.Name, "docker", "compose", name, unitName),
				ProviderPayload: unitDesired,
			})
			operations = append(operations, Operation{
				Address:        daemonReloadAddress,
				Action:         "run",
				Summary:        "reload systemd manager configuration",
				DependsOn:      []string{unitAddress},
				TriggeredBy:    []string{unitAddress},
				CommandPreview: "systemctl daemon-reload",
				Source:         compose.Source,
			})
			serviceDesired := dockerComposeServiceDesired(compose, unitName)
			serviceDeps := []string{unitAddress, daemonReloadAddress}
			if projectServiceAddress != "" {
				serviceDeps = append(serviceDeps, projectServiceAddress)
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         serviceAddress,
				Kind:            "service",
				Summary:         "manage docker compose service " + compose.Service.Name,
				Source:          compose.Source,
				Desired:         serviceDesired,
				DependsOn:       dedupeStrings(serviceDeps),
				ProviderType:    "service",
				ProviderAddress: "service." + providerName(host.Name, "docker", "compose", name),
				ProviderPayload: serviceDesired,
			})
		}

		projectAddress := prefix + ".project"
		projectDeps := []string{validateAddress}
		if serviceAddress != "" {
			projectDeps = append(projectDeps, serviceAddress)
		} else if projectServiceAddress != "" {
			projectDeps = append(projectDeps, projectServiceAddress)
		}
		projectDesired := dockerComposeProjectDesired(compose)
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         projectAddress,
			Kind:            "docker_compose_project",
			Summary:         "converge docker compose project " + compose.Project,
			Source:          compose.Source,
			Desired:         projectDesired,
			DependsOn:       dedupeStrings(projectDeps),
			ProviderType:    "docker_compose_project",
			ProviderAddress: "docker_compose_project." + providerName(host.Name, name),
			ProviderPayload: projectDesired,
		})
	}
	return nodes, operations, validateAddresses, nil
}

func composeSystemdUnitName(compose ir.DockerComposeSpec) string {
	return serviceUnitName(compose.Service.Name)
}

func serviceUnitName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".service"
}

func dockerComposeSystemdUnitDesired(compose ir.DockerComposeSpec, unitName string) map[string]any {
	content := dockerComposeSystemdUnitContent(compose)
	summary := contentSummary([]byte(content))
	return map[string]any{
		"name":    unitName,
		"path":    "/etc/systemd/system/" + unitName,
		"content": content,
		"owner":   "root",
		"group":   "root",
		"mode":    "0644",
		"ensure":  "present",
		"summary": summary,
	}
}

func dockerComposeSystemdUnitContent(compose ir.DockerComposeSpec) string {
	lines := []string{
		"[Unit]",
		"Description=DebianForm Compose Project " + compose.Project,
		"Requires=docker.service",
		"After=" + strings.Join(compose.After, " "),
		"",
		"[Service]",
		"Type=oneshot",
		"RemainAfterExit=yes",
		"WorkingDirectory=" + compose.Directory,
		"ExecStart=" + dockerComposeUnitCommand(compose, "up", "-d"),
		"ExecStop=" + dockerComposeUnitCommand(compose, "stop"),
		"TimeoutStartSec=0",
		"",
		"[Install]",
		"WantedBy=" + strings.Join(compose.WantedBy, " "),
	}
	return strings.Join(lines, "\n") + "\n"
}

func dockerComposeUnitCommand(compose ir.DockerComposeSpec, command ...string) string {
	args := []string{
		"/usr/bin/docker",
		"compose",
		"-p",
		compose.Project,
	}
	if compose.File != nil {
		args = append(args, "-f", compose.File.Path)
	}
	args = append(args, command...)
	return quoteCommand(args)
}

func dockerComposeServiceDesired(compose ir.DockerComposeSpec, unitName string) map[string]any {
	state := "stopped"
	enabled := false
	switch compose.State {
	case "running":
		enabled = true
		state = "running"
	case "stopped":
		enabled = true
		state = "stopped"
	case "absent":
		enabled = false
		state = "stopped"
	}
	return map[string]any{
		"name":    compose.Service.Name,
		"unit":    unitName,
		"enabled": enabled,
		"state":   state,
	}
}

func dockerComposeProjectDesired(compose ir.DockerComposeSpec) map[string]any {
	desired := map[string]any{
		"name":           compose.Name,
		"directory":      compose.Directory,
		"project":        compose.Project,
		"state":          compose.State,
		"pull":           compose.Pull,
		"recreate":       compose.Recreate,
		"remove_orphans": compose.RemoveOrphans,
	}
	if compose.File != nil {
		desired["files"] = []string{compose.File.Path}
	}
	if len(compose.EnvFiles) > 0 {
		envFiles := make([]string, 0, len(compose.EnvFiles))
		for _, label := range sortedKeys(compose.EnvFiles) {
			envFiles = append(envFiles, compose.EnvFiles[label].Path)
		}
		desired["env_files"] = envFiles
	}
	return desired
}

func dockerComposeFileNode(hostName, address, composeName string, item ir.DockerComposeFileSpec, summary string, deps []string) (string, Node) {
	desired, payload := fileResourceDesiredPayload(fileResourceSpec{
		Path:       item.Path,
		Content:    item.Content,
		SourcePath: item.SourcePath,
		Owner:      item.Owner,
		Group:      item.Group,
		Mode:       item.Mode,
		Sensitive:  item.Sensitive,
		Ensure:     "present",
		Summary:    item.Summary,
	})
	desired["compose"] = composeName
	payload["compose"] = composeName
	node := Node{
		Host:            hostName,
		Address:         address,
		Kind:            "file",
		Summary:         summary,
		Source:          item.Source,
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "file",
		ProviderAddress: "file." + providerName(hostName, item.Path),
		ProviderPayload: payload,
	}
	return address, node
}

func dockerComposeConfigCommand(compose ir.DockerComposeSpec) string {
	args := []string{
		"docker",
		"compose",
		"-p",
		compose.Project,
	}
	if compose.File != nil {
		args = append(args, "-f", compose.File.Path)
	}
	args = append(args, "config")
	return quoteCommand(args)
}
