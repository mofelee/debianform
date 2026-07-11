package merge

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func dockerSpec(docker parser.Value) (*ir.DockerSpec, error) {
	enable, ok, err := boolField(docker, "enable")
	if err != nil {
		return nil, err
	}
	if !ok {
		enable = false
	}
	users, err := dockerUsersField(docker)
	if err != nil {
		return nil, err
	}
	packageSpec, err := dockerPackageSpec(docker)
	if err != nil {
		return nil, err
	}
	serviceSpec, err := dockerServiceSpec(docker)
	if err != nil {
		return nil, err
	}
	daemon, err := dockerDaemonSpec(docker)
	if err != nil {
		return nil, err
	}
	composes, err := dockerComposeSpecs(docker)
	if err != nil {
		return nil, err
	}
	return &ir.DockerSpec{
		Enable:   enable,
		Package:  packageSpec,
		Service:  serviceSpec,
		Daemon:   daemon,
		Users:    users,
		Composes: composes,
		Source:   docker.Source,
	}, nil
}

func dockerUsersField(docker parser.Value) ([]string, error) {
	list, ok, err := listField(docker, "users")
	if err != nil || !ok {
		return nil, err
	}
	out := make([]string, 0, len(list.List))
	seen := map[string]struct{}{}
	for _, item := range list.List {
		value, ok := item.StringValue()
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s:%d:%s: docker users entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate docker users entry %q", item.Source.File, item.Source.Line, item.Source.Path, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func dockerPackageSpec(docker parser.Value) (ir.DockerPackageSpec, error) {
	packageBlock, ok, err := mapField(docker, "package")
	if err != nil {
		return ir.DockerPackageSpec{}, err
	}
	sourceRef := docker.Source
	source := "official"
	channel := "stable"
	removeConflicts := "auto"
	repositoryURL := ir.DockerOfficialRepositoryURL
	gpgURL := ir.DockerOfficialGPGURL
	gpgSHA256 := ""
	var version *string
	var hasRepositoryURL bool
	var hasGPGURL bool
	var hasGPGSHA256 bool
	if ok {
		sourceRef = packageBlock.Source
		if value, hasValue, err := stringField(packageBlock, "source"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			source = value
		}
		if value, hasValue, err := stringField(packageBlock, "channel"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			channel = value
		}
		if value, hasValue, err := optionalStringField(packageBlock, "version"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			version = value
		}
		if value, hasValue, err := stringField(packageBlock, "repository_url"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			repositoryURL = value
			hasRepositoryURL = true
		}
		if value, hasValue, err := stringField(packageBlock, "gpg_url"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			gpgURL = value
			hasGPGURL = true
		}
		if value, hasValue, err := stringField(packageBlock, "gpg_sha256"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			gpgSHA256 = value
			hasGPGSHA256 = true
		}
		if value, hasValue, err := dockerRemoveConflictsField(packageBlock, "remove_conflicts"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			removeConflicts = value
		}
	}
	if source == "debian" {
		errSource := packageBlock.Map["source"].Source
		return ir.DockerPackageSpec{}, fmt.Errorf("%s:%d:%s: docker package source %q is no longer supported; omit source for Docker's official repository, or use source = \"none\" or source = \"custom\"", errSource.File, errSource.Line, errSource.Path, source)
	}
	if !stringIn(source, "official", "none", "custom") {
		errSource := sourceRef
		if ok {
			errSource = packageBlock.Map["source"].Source
		}
		return ir.DockerPackageSpec{}, enumError(errSource, "official, none, or custom")
	}
	if channel == "" {
		return ir.DockerPackageSpec{}, fmt.Errorf("%s:%d:%s.channel: docker package channel must be non-empty", sourceRef.File, sourceRef.Line, sourceRef.Path)
	}
	if source != "official" {
		if ok {
			for _, name := range []string{"repository_url", "gpg_url", "gpg_sha256"} {
				if value, exists := packageBlock.Map[name]; exists {
					return ir.DockerPackageSpec{}, fmt.Errorf("%s:%d:%s: docker package %s is only valid when source = \"official\" (current source = %q)", value.Source.File, value.Source.Line, value.Source.Path, name, source)
				}
			}
		}
		repositoryURL = ""
		gpgURL = ""
		gpgSHA256 = ""
	} else {
		if hasRepositoryURL && repositoryURL == "" {
			return ir.DockerPackageSpec{}, fmt.Errorf("%s:%d:%s: docker package repository_url must be non-empty", packageBlock.Map["repository_url"].Source.File, packageBlock.Map["repository_url"].Source.Line, packageBlock.Map["repository_url"].Source.Path)
		}
		if hasGPGURL && gpgURL == "" {
			return ir.DockerPackageSpec{}, fmt.Errorf("%s:%d:%s: docker package gpg_url must be non-empty", packageBlock.Map["gpg_url"].Source.File, packageBlock.Map["gpg_url"].Source.Line, packageBlock.Map["gpg_url"].Source.Path)
		}
		if hasGPGSHA256 {
			if !sha256Pattern.MatchString(gpgSHA256) {
				return ir.DockerPackageSpec{}, fmt.Errorf("%s:%d:%s: docker package gpg_sha256 must be a 64 character hex string", packageBlock.Map["gpg_sha256"].Source.File, packageBlock.Map["gpg_sha256"].Source.Line, packageBlock.Map["gpg_sha256"].Source.Path)
			}
			gpgSHA256 = strings.ToLower(gpgSHA256)
		} else if gpgURL == ir.DockerOfficialGPGURL {
			gpgSHA256 = ir.DockerOfficialGPGSHA256
		}
	}
	return ir.DockerPackageSpec{
		Source:          source,
		Channel:         channel,
		Version:         version,
		RepositoryURL:   repositoryURL,
		GPGURL:          gpgURL,
		GPGSHA256:       gpgSHA256,
		RemoveConflicts: removeConflicts,
		SourceRef:       sourceRef,
	}, nil
}

func dockerServiceSpec(docker parser.Value) (ir.DockerServiceSpec, error) {
	serviceBlock, ok, err := mapField(docker, "service")
	if err != nil {
		return ir.DockerServiceSpec{}, err
	}
	sourceRef := docker.Source
	enable := true
	state := "running"
	if ok {
		sourceRef = serviceBlock.Source
		if value, hasValue, err := boolField(serviceBlock, "enable"); err != nil {
			return ir.DockerServiceSpec{}, err
		} else if hasValue {
			enable = value
		}
		if value, hasValue, err := stringField(serviceBlock, "state"); err != nil {
			return ir.DockerServiceSpec{}, err
		} else if hasValue {
			state = value
		}
	}
	if !stringIn(state, "running", "stopped") {
		errSource := sourceRef
		if ok {
			errSource = serviceBlock.Map["state"].Source
		}
		return ir.DockerServiceSpec{}, enumError(errSource, "running or stopped")
	}
	return ir.DockerServiceSpec{
		Enable:    enable,
		State:     state,
		Name:      "docker.service",
		SourceRef: sourceRef,
	}, nil
}

func dockerDaemonSpec(docker parser.Value) (*ir.DockerDaemonSpec, error) {
	daemon, ok, err := mapField(docker, "daemon")
	if err != nil || !ok {
		return nil, err
	}
	settingsValue, ok := daemon.Map["settings"]
	if !ok {
		settingsValue = parser.MapValue(nil, ir.SourceRef{File: daemon.Source.File, Line: daemon.Source.Line, Path: daemon.Source.Path + ".settings"})
	}
	if !settingsValue.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: docker daemon settings must be a map", settingsValue.Source.File, settingsValue.Source.Line, settingsValue.Source.Path)
	}
	settings, err := jsonCompatibleAny(settingsValue)
	if err != nil {
		return nil, err
	}
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s:%d:%s: docker daemon settings must be a map", settingsValue.Source.File, settingsValue.Source.Line, settingsValue.Source.Path)
	}
	summary, err := jsonContentSummary(settingsMap, daemon.Source)
	if err != nil {
		return nil, err
	}
	return &ir.DockerDaemonSpec{Settings: settingsMap, Source: daemon.Source, Summary: summary}, nil
}

func dockerComposeSpecs(docker parser.Value) (map[string]ir.DockerComposeSpec, error) {
	objects, ok, err := objectCollection(docker, "compose")
	if err != nil || !ok {
		return map[string]ir.DockerComposeSpec{}, err
	}
	out := make(map[string]ir.DockerComposeSpec, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: docker compose label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if err := validateDockerStableName("docker compose label", name, item.Source); err != nil {
			return nil, err
		}
		compose, err := dockerComposeSpec(name, item)
		if err != nil {
			return nil, err
		}
		out[name] = compose
	}
	return out, nil
}

func dockerComposeSpec(name string, item parser.Value) (ir.DockerComposeSpec, error) {
	enable, ok, err := boolField(item, "enable")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !ok {
		enable = true
	}
	state, err := stringFieldDefault(item, "state", "running")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !stringIn(state, "running", "stopped", "absent") {
		return ir.DockerComposeSpec{}, enumError(item.Map["state"].Source, "running, stopped, or absent")
	}
	directory, _, err := stringField(item, "directory")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if enable && (directory == "" || !filepath.IsAbs(directory)) {
		return ir.DockerComposeSpec{}, fmt.Errorf("%s:%d:%s.directory: docker compose directory must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	project, err := stringFieldDefault(item, "project", name)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if project == "" {
		return ir.DockerComposeSpec{}, fmt.Errorf("%s:%d:%s.project: docker compose project must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	projectSource := item.Source
	if value, ok := item.Map["project"]; ok {
		projectSource = value.Source
	}
	if err := validateDockerStableName("docker compose project", project, projectSource); err != nil {
		return ir.DockerComposeSpec{}, err
	}
	pull, err := stringFieldDefault(item, "pull", "missing")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !stringIn(pull, "never", "missing", "always") {
		return ir.DockerComposeSpec{}, enumError(item.Map["pull"].Source, "never, missing, or always")
	}
	recreate, err := stringFieldDefault(item, "recreate", "auto")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !stringIn(recreate, "auto", "always", "never") {
		return ir.DockerComposeSpec{}, enumError(item.Map["recreate"].Source, "auto, always, or never")
	}
	removeOrphans, ok, err := boolField(item, "remove_orphans")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !ok {
		removeOrphans = false
	}
	after, err := stringListField(item, "after")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if len(after) == 0 {
		after = []string{"docker.service", "network-online.target"}
	}
	wantedBy, err := stringListField(item, "wanted_by")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if len(wantedBy) == 0 {
		wantedBy = []string{"multi-user.target"}
	}
	composeFile, err := dockerComposeMainFileSpec(item)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	envFiles, err := dockerComposeEnvFileSpecs(item)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if enable && composeFile == nil {
		return ir.DockerComposeSpec{}, fmt.Errorf("%s:%d:%s.file: docker compose file block is required", item.Source.File, item.Source.Line, item.Source.Path)
	}
	service, err := dockerComposeServiceSpec(name, item)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	return ir.DockerComposeSpec{
		Name:          name,
		Enable:        enable,
		State:         state,
		Directory:     directory,
		Project:       project,
		File:          composeFile,
		EnvFiles:      envFiles,
		Pull:          pull,
		Recreate:      recreate,
		RemoveOrphans: removeOrphans,
		Service:       service,
		After:         after,
		WantedBy:      wantedBy,
		Source:        item.Source,
	}, nil
}

func dockerComposeMainFileSpec(compose parser.Value) (*ir.DockerComposeFileSpec, error) {
	fileBlock, ok, err := mapField(compose, "file")
	if err != nil || !ok {
		return nil, err
	}
	file, err := dockerComposeFileSpec("", fileBlock, "0644", false)
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func dockerComposeEnvFileSpecs(compose parser.Value) (map[string]ir.DockerComposeFileSpec, error) {
	objects, ok, err := objectCollection(compose, "env_file")
	if err != nil || !ok {
		return map[string]ir.DockerComposeFileSpec{}, err
	}
	out := make(map[string]ir.DockerComposeFileSpec, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		if label == "" {
			return nil, fmt.Errorf("%s:%d:%s: docker compose env_file label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		file, err := dockerComposeFileSpec(label, item, "0600", true)
		if err != nil {
			return nil, err
		}
		out[label] = file
	}
	return out, nil
}

func dockerComposeFileSpec(label string, item parser.Value, defaultMode string, defaultSensitive bool) (ir.DockerComposeFileSpec, error) {
	path, ok, err := stringField(item, "path")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	if !ok || path == "" || !filepath.IsAbs(path) {
		return ir.DockerComposeFileSpec{}, fmt.Errorf("%s:%d:%s.path: docker compose file path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	if hasContent == hasSource {
		return ir.DockerComposeFileSpec{}, fmt.Errorf("%s:%d:%s: docker compose file requires exactly one of content or source", item.Source.File, item.Source.Line, item.Source.Path)
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	mode, err := modeFieldDefault(item, "mode", defaultMode)
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	sensitive := defaultSensitive
	if hasContent && contentNeedsRedaction(item.Map["content"]) {
		sensitive = true
	}
	out := ir.DockerComposeFileSpec{
		Label:      label,
		Path:       path,
		Content:    content,
		SourcePath: resolvePath(item.Source.File, sourcePath),
		Owner:      owner,
		Group:      group,
		Mode:       mode,
		Sensitive:  sensitive,
		Source:     item.Source,
	}
	if hasContent {
		out.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(out.SourcePath, item.Source)
		if err != nil {
			return ir.DockerComposeFileSpec{}, err
		}
		out.Summary = summary
	}
	return out, nil
}

func dockerComposeServiceSpec(name string, compose parser.Value) (ir.DockerComposeServiceSpec, error) {
	service, ok, err := mapField(compose, "service")
	if err != nil {
		return ir.DockerComposeServiceSpec{}, err
	}
	enable := true
	unitName := "debianform-compose-" + name
	if ok {
		if value, hasValue, err := boolField(service, "enable"); err != nil {
			return ir.DockerComposeServiceSpec{}, err
		} else if hasValue {
			enable = value
		}
		if value, hasValue, err := stringField(service, "name"); err != nil {
			return ir.DockerComposeServiceSpec{}, err
		} else if hasValue {
			unitName = value
		}
	}
	if unitName == "" {
		return ir.DockerComposeServiceSpec{}, fmt.Errorf("%s:%d:%s.service.name: docker compose service name must be non-empty", compose.Source.File, compose.Source.Line, compose.Source.Path)
	}
	serviceSource := compose.Source
	if ok {
		if value, exists := service.Map["name"]; exists {
			serviceSource = value.Source
		}
	}
	if err := validateDockerStableName("docker compose service name", unitName, serviceSource); err != nil {
		return ir.DockerComposeServiceSpec{}, err
	}
	return ir.DockerComposeServiceSpec{Enable: enable, Name: unitName}, nil
}
