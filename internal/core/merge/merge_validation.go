package merge

import (
	"fmt"
	"reflect"

	"github.com/mofelee/debianform/internal/core/ir"
)

func validateHostSpec(spec ir.HostSpec) error {
	files := map[string]ir.SourceRef{}
	secrets := map[string]ir.SourceRef{}
	repositories := map[string]ir.APTRepositorySpec{}
	packages := map[string]ir.PackageItem{}
	directories := map[string]ir.ManagedDirectory{}
	groups := map[string]ir.ManagedGroup{}
	users := map[string]ir.ManagedUser{}
	units := map[string]ir.SystemdUnit{}
	services := map[string]ir.ManagedService{}

	for path, file := range spec.Files.Files {
		files[path] = file.Source
	}
	for path, secret := range spec.Secrets.Files {
		secrets[path] = secret.Source
		if fileSource, exists := files[path]; exists {
			return fmt.Errorf("%s:%d:%s: file path %q conflicts with secret declared at %s:%d:%s", fileSource.File, fileSource.Line, fileSource.Path, path, secret.Source.File, secret.Source.Line, secret.Source.Path)
		}
	}
	for name, repository := range spec.APT.Repositories {
		repositories[name] = repository
	}
	for _, label := range sortedKeys(spec.APT.SourceFiles) {
		item := spec.APT.SourceFiles[label]
		if previous, exists := files[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: apt source_file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: apt source_file path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		files[item.Path] = item.Source
	}
	for _, pkg := range spec.Packages.Install {
		packages[pkg.Name] = pkg
	}
	for path, directory := range spec.Directories.Directories {
		directories[path] = directory
	}
	for name, group := range spec.Groups.Groups {
		groups[name] = group
	}
	for name, user := range spec.Users.Users {
		users[name] = user
	}
	for _, unit := range spec.Systemd.Units {
		units[unit.Name] = unit
		files[unit.Path] = unit.Source
	}
	for name, timer := range spec.Systemd.Timers {
		unit := timer.Unit
		if previous, exists := units[unit.Name]; exists {
			return fmt.Errorf("%s:%d:%s: systemd timer %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		if previous, exists := files[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd timer path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd timer path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		units[name] = unit
		files[unit.Path] = unit.Source
	}
	if spec.Systemd.Resolved != nil {
		unit := spec.Systemd.Resolved.Unit
		if previous, exists := files[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd resolved path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd resolved path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		files[unit.Path] = unit.Source
	}
	if spec.Systemd.Journald != nil {
		unit := spec.Systemd.Journald.Unit
		if previous, exists := files[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd journald path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd journald path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		files[unit.Path] = unit.Source
	}
	if spec.Systemd.Networkd != nil {
		if err := validateNetworkdSpecPaths(*spec.Systemd.Networkd, files, secrets); err != nil {
			return err
		}
		for _, label := range sortedKeys(spec.Systemd.Networkd.NetDevs) {
			item := spec.Systemd.Networkd.NetDevs[label]
			files[item.Path] = item.Source
		}
		for _, label := range sortedKeys(spec.Systemd.Networkd.Networks) {
			item := spec.Systemd.Networkd.Networks[label]
			files[item.Path] = item.Source
		}
	}
	for name, service := range spec.Services.Services {
		services[name] = service
	}
	if spec.Nftables.Main != nil {
		if err := validateNftablesPath(spec.Nftables.Main, files, secrets); err != nil {
			return err
		}
		files[spec.Nftables.Main.Path] = spec.Nftables.Main.Source
	}
	for _, label := range sortedKeys(spec.Nftables.Files) {
		item := spec.Nftables.Files[label]
		if err := validateNftablesPath(&item, files, secrets); err != nil {
			return err
		}
		files[item.Path] = item.Source
	}
	if spec.Docker != nil {
		if spec.Docker.Daemon != nil {
			daemonSource := spec.Docker.Daemon.Source
			if previous, exists := files["/etc/docker/daemon.json"]; exists {
				return fmt.Errorf("%s:%d:%s: docker daemon path %q conflicts with file declared at %s:%d:%s", daemonSource.File, daemonSource.Line, daemonSource.Path, "/etc/docker/daemon.json", previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets["/etc/docker/daemon.json"]; exists {
				return fmt.Errorf("%s:%d:%s: docker daemon path %q conflicts with secret declared at %s:%d:%s", daemonSource.File, daemonSource.Line, daemonSource.Path, "/etc/docker/daemon.json", previous.File, previous.Line, previous.Path)
			}
			files["/etc/docker/daemon.json"] = daemonSource
		}
		for _, name := range sortedKeys(spec.Docker.Composes) {
			compose := spec.Docker.Composes[name]
			if compose.Service.Enable {
				unitName := serviceUnitName(compose.Service.Name)
				unitPath := "/etc/systemd/system/" + unitName
				if previous, exists := files[unitPath]; exists {
					return fmt.Errorf("%s:%d:%s.service.name: docker compose systemd unit path %q conflicts with file declared at %s:%d:%s", compose.Source.File, compose.Source.Line, compose.Source.Path, unitPath, previous.File, previous.Line, previous.Path)
				}
				if previous, exists := secrets[unitPath]; exists {
					return fmt.Errorf("%s:%d:%s.service.name: docker compose systemd unit path %q conflicts with secret declared at %s:%d:%s", compose.Source.File, compose.Source.Line, compose.Source.Path, unitPath, previous.File, previous.Line, previous.Path)
				}
				files[unitPath] = compose.Source
			}
			if compose.File != nil {
				if err := validateDockerComposePath("docker compose file", *compose.File, files, secrets); err != nil {
					return err
				}
				files[compose.File.Path] = compose.File.Source
			}
			for _, label := range sortedKeys(compose.EnvFiles) {
				envFile := compose.EnvFiles[label]
				if err := validateDockerComposePath("docker compose env_file", envFile, files, secrets); err != nil {
					return err
				}
				files[envFile.Path] = envFile.Source
			}
		}
	}

	for _, component := range spec.Components {
		if component.Install != nil {
			path := component.Install.Path
			if previous, exists := files[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q artifact path %q conflicts with file declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q artifact path %q conflicts with secret declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := directories[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q artifact path %q conflicts with directory declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			files[path] = component.Install.Source
		}
		for path, file := range component.Files.Files {
			if previous, exists := files[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q file path %q conflicts with file declared at %s:%d:%s", file.Source.File, file.Source.Line, file.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q file path %q conflicts with secret declared at %s:%d:%s", file.Source.File, file.Source.Line, file.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			files[path] = file.Source
		}
		for path, secret := range component.Secrets.Files {
			if previous, exists := files[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q secret path %q conflicts with file declared at %s:%d:%s", secret.Source.File, secret.Source.Line, secret.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q secret path %q conflicts with secret declared at %s:%d:%s", secret.Source.File, secret.Source.Line, secret.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			secrets[path] = secret.Source
		}
		for name, repository := range component.APT.Repositories {
			if previous, exists := repositories[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q apt.repository %q conflicts with repository declared at %s:%d:%s", repository.Source.File, repository.Source.Line, repository.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			repositories[name] = repository
		}
		for _, label := range sortedKeys(component.APT.SourceFiles) {
			item := component.APT.SourceFiles[label]
			if previous, exists := files[item.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q apt source_file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, component.Name, item.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[item.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q apt source_file path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, component.Name, item.Path, previous.File, previous.Line, previous.Path)
			}
			files[item.Path] = item.Source
		}
		for _, pkg := range component.Packages.Install {
			if previous, exists := packages[pkg.Name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q package %q conflicts with package declared at %s:%d:%s", pkg.Source.File, pkg.Source.Line, pkg.Source.Path, component.Name, pkg.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			packages[pkg.Name] = pkg
		}
		for path, directory := range component.Directories.Directories {
			if previous, exists := directories[path]; exists {
				if !sameManagedDirectory(directory, previous) {
					return fmt.Errorf("%s:%d:%s: component %q directory %q conflicts with directory declared at %s:%d:%s", directory.Source.File, directory.Source.Line, directory.Source.Path, component.Name, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
				}
				continue
			}
			directories[path] = directory
		}
		for name, group := range component.Groups.Groups {
			if previous, exists := groups[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q group %q conflicts with group declared at %s:%d:%s", group.Source.File, group.Source.Line, group.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			groups[name] = group
		}
		for name, user := range component.Users.Users {
			if previous, exists := users[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q user %q conflicts with user declared at %s:%d:%s", user.Source.File, user.Source.Line, user.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			users[name] = user
		}
		for name, unit := range component.Systemd.Units {
			if previous, exists := units[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd unit %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd unit path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd unit path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			units[unit.Name] = unit
			files[unit.Path] = unit.Source
		}
		for name, timer := range component.Systemd.Timers {
			unit := timer.Unit
			if previous, exists := units[unit.Name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd timer %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd timer path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd timer path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			units[name] = unit
			files[unit.Path] = unit.Source
		}
		if component.Systemd.Resolved != nil {
			unit := component.Systemd.Resolved.Unit
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd resolved path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd resolved path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			files[unit.Path] = unit.Source
		}
		if component.Systemd.Journald != nil {
			unit := component.Systemd.Journald.Unit
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd journald path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd journald path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			files[unit.Path] = unit.Source
		}
		if component.Systemd.Networkd != nil {
			if err := validateNetworkdSpecPaths(*component.Systemd.Networkd, files, secrets); err != nil {
				return err
			}
			for _, label := range sortedKeys(component.Systemd.Networkd.NetDevs) {
				item := component.Systemd.Networkd.NetDevs[label]
				files[item.Path] = item.Source
			}
			for _, label := range sortedKeys(component.Systemd.Networkd.Networks) {
				item := component.Systemd.Networkd.Networks[label]
				files[item.Path] = item.Source
			}
		}
		for name, service := range component.Services.Services {
			if previous, exists := services[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q service %q conflicts with service declared at %s:%d:%s", service.Source.File, service.Source.Line, service.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			services[name] = service
		}
	}

	for _, user := range users {
		if user.PrimaryGroup == "" {
			continue
		}
		if _, ok := groups[user.PrimaryGroup]; !ok && user.PrimaryGroup != user.Name {
			return fmt.Errorf("%s:%d:%s: user %q references missing primary group %q", user.Source.File, user.Source.Line, user.Source.Path, user.Name, user.PrimaryGroup)
		}
	}
	for _, pkg := range packages {
		for _, repo := range pkg.Repositories {
			repository, ok := repositories[repo]
			if !ok {
				return fmt.Errorf("%s:%d:%s: package %q references missing apt.repository %q", pkg.Source.File, pkg.Source.Line, pkg.Source.Path, pkg.Name, repo)
			}
			if repository.Ensure == "absent" {
				return fmt.Errorf("%s:%d:%s: package %q references absent apt.repository %q", pkg.Source.File, pkg.Source.Line, pkg.Source.Path, pkg.Name, repo)
			}
		}
	}
	return nil
}

func sameManagedDirectory(a, b ir.ManagedDirectory) bool {
	return a.Path == b.Path &&
		a.Owner == b.Owner &&
		a.Group == b.Group &&
		a.Mode == b.Mode &&
		a.Ensure == b.Ensure &&
		reflect.DeepEqual(a.Lifecycle, b.Lifecycle)
}

func validateNftablesPath(item *ir.NftablesFileSpec, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	if item == nil {
		return nil
	}
	if previous, exists := files[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: nftables file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
	}
	if previous, exists := secrets[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: nftables file path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
	}
	return nil
}

func validateDockerComposePath(kind string, item ir.DockerComposeFileSpec, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	if previous, exists := files[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, kind, item.Path, previous.File, previous.Line, previous.Path)
	}
	if previous, exists := secrets[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, kind, item.Path, previous.File, previous.Line, previous.Path)
	}
	return nil
}

func validateNetworkdSpecPaths(spec ir.NetworkdSpec, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	seen := map[string]ir.SourceRef{}
	for _, label := range sortedKeys(spec.NetDevs) {
		item := spec.NetDevs[label]
		if err := validateNetworkdPath("networkd netdev", item.Path, item.Source, files, secrets); err != nil {
			return err
		}
		if previous, exists := seen[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: networkd netdev path %q conflicts with networkd file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		seen[item.Path] = item.Source
	}
	for _, label := range sortedKeys(spec.Networks) {
		item := spec.Networks[label]
		if err := validateNetworkdPath("networkd network", item.Path, item.Source, files, secrets); err != nil {
			return err
		}
		if previous, exists := seen[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: networkd network path %q conflicts with networkd file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		seen[item.Path] = item.Source
	}
	return nil
}

func validateNetworkdPath(kind string, path string, source ir.SourceRef, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	if previous, exists := files[path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with file declared at %s:%d:%s", source.File, source.Line, source.Path, kind, path, previous.File, previous.Line, previous.Path)
	}
	if previous, exists := secrets[path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with secret declared at %s:%d:%s", source.File, source.Line, source.Path, kind, path, previous.File, previous.Line, previous.Path)
	}
	return nil
}
