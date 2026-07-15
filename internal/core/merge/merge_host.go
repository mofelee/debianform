package merge

import (
	"fmt"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func (c *compiler) buildHostSpec(host parser.Host, raw parser.Value) (ir.HostSpec, error) {
	spec := ir.HostSpec{
		Name:   host.Name,
		Source: host.Source,
		SSH: ir.SSHSpec{
			Host:   host.Name,
			Port:   22,
			User:   "root",
			Source: host.Source,
		},
		State: ir.StateSpec{
			Path:     "/var/lib/debianform/state/" + host.Name + ".json",
			LockPath: "/var/lock/debianform/state/" + host.Name + ".lock",
			Source:   host.Source,
		},
		System: ir.SystemSpec{
			Source: host.Source,
		},
		Kernel: ir.KernelSpec{
			Sysctl: map[string]ir.SysctlSpec{},
		},
		APT: ir.APTSpec{
			Repositories: map[string]ir.APTRepositorySpec{},
			SourceFiles:  map[string]ir.APTSourceFileSpec{},
		},
		Files: ir.FileSpec{
			Files: map[string]ir.ManagedFile{},
		},
		Secrets: ir.SecretSpec{
			Files: map[string]ir.SecretFile{},
		},
		Directories: ir.DirectorySpec{
			Directories: map[string]ir.ManagedDirectory{},
		},
		Groups: ir.GroupSpec{
			Groups: map[string]ir.ManagedGroup{},
		},
		Users: ir.UserSpec{
			Users: map[string]ir.ManagedUser{},
		},
		Systemd: ir.SystemdSpec{
			Units:  map[string]ir.SystemdUnit{},
			Timers: map[string]ir.SystemdTimer{},
		},
		Services: ir.ServiceSpec{
			Services: map[string]ir.ManagedService{},
		},
		Nftables: ir.NftablesSpec{
			Files: map[string]ir.NftablesFileSpec{},
		},
	}

	if ssh, ok, err := mapField(raw, "ssh"); err != nil {
		return spec, err
	} else if ok {
		spec.SSH.Source = ssh.Source
		if value, ok, err := stringField(ssh, "host"); err != nil {
			return spec, err
		} else if ok {
			spec.SSH.Host = value
		}
		if value, ok, err := intField(ssh, "port"); err != nil {
			return spec, err
		} else if ok {
			spec.SSH.Port = value
		}
		if value, ok, err := stringField(ssh, "user"); err != nil {
			return spec, err
		} else if ok {
			if value != "" && value != "root" {
				return spec, fmt.Errorf("%s:%d:%s.user: DebianForm manages target hosts as root; ssh.user must be \"root\" or omitted", ssh.Source.File, ssh.Source.Line, ssh.Source.Path)
			}
			spec.SSH.User = value
		}
		if value, ok, err := stringField(ssh, "identity_file"); err != nil {
			return spec, err
		} else if ok {
			spec.SSH.IdentityFile = value
		}
	}

	if state, ok, err := mapField(raw, "state"); err != nil {
		return spec, err
	} else if ok {
		spec.State.Source = state.Source
		if value, ok, err := stringField(state, "path"); err != nil {
			return spec, err
		} else if ok {
			spec.State.Path = value
		}
		if value, ok, err := stringField(state, "lock_path"); err != nil {
			return spec, err
		} else if ok {
			spec.State.LockPath = value
		}
	}

	if system, ok, err := mapField(raw, "system"); err != nil {
		return spec, err
	} else if ok {
		spec.System.Source = system.Source
		if value, ok, err := stringField(system, "hostname"); err != nil {
			return spec, err
		} else if ok {
			spec.System.Hostname = value
			spec.System.HostnameSet = true
		}
		if value, exists := system.Map["architecture"]; exists {
			return spec, fmt.Errorf("%s:%d:%s: system.architecture is no longer supported; use platform.architecture", value.Source.File, value.Source.Line, value.Source.Path)
		}
		if value, exists := system.Map["codename"]; exists {
			return spec, fmt.Errorf("%s:%d:%s: system.codename is no longer supported; use platform.codename", value.Source.File, value.Source.Line, value.Source.Path)
		}
		if value, ok, err := stringField(system, "timezone"); err != nil {
			return spec, err
		} else if ok {
			if err := validateSystemTimezone(value, system.Map["timezone"].Source); err != nil {
				return spec, err
			}
			spec.System.Timezone = value
			spec.System.TimezoneSet = true
		}
		if value, ok, err := stringField(system, "locale"); err != nil {
			return spec, err
		} else if ok {
			if err := validateSystemLocale(value, system.Map["locale"].Source); err != nil {
				return spec, err
			}
			spec.System.Locale = value
			spec.System.LocaleSet = true
		}
	}

	if platform, ok, err := mapField(raw, "platform"); err != nil {
		return spec, err
	} else if ok {
		spec.Platform = &ir.PlatformSpec{Source: platform.Source}
		if value, ok, err := stringField(platform, "distribution"); err != nil {
			return spec, err
		} else if ok {
			if value == "" {
				return spec, fmt.Errorf("%s:%d:%s.distribution: platform.distribution must be non-empty", platform.Source.File, platform.Source.Line, platform.Source.Path)
			}
			spec.Platform.Distribution = value
		}
		if value, ok, err := stringField(platform, "version"); err != nil {
			return spec, err
		} else if ok {
			if value == "" {
				return spec, fmt.Errorf("%s:%d:%s.version: platform.version must be non-empty", platform.Source.File, platform.Source.Line, platform.Source.Path)
			}
			spec.Platform.Version = value
		}
		if value, ok, err := stringField(platform, "architecture"); err != nil {
			return spec, err
		} else if ok {
			if value == "" {
				return spec, fmt.Errorf("%s:%d:%s.architecture: platform.architecture must be non-empty", platform.Source.File, platform.Source.Line, platform.Source.Path)
			}
			spec.Platform.Architecture = value
		}
		if value, ok, err := stringField(platform, "codename"); err != nil {
			return spec, err
		} else if ok {
			if value == "" {
				return spec, fmt.Errorf("%s:%d:%s.codename: platform.codename must be non-empty", platform.Source.File, platform.Source.Line, platform.Source.Path)
			}
			spec.Platform.Codename = value
		}
		if err := validateDeclaredPlatform(*spec.Platform); err != nil {
			return spec, err
		}
	}

	if kernel, ok, err := mapField(raw, "kernel"); err != nil {
		return spec, err
	} else if ok {
		spec.Kernel.Source = kernel.Source
		modules, err := moduleSpecs(kernel)
		if err != nil {
			return spec, err
		}
		spec.Kernel.Modules = modules
		sysctl, err := sysctlSpecs(kernel)
		if err != nil {
			return spec, err
		}
		spec.Kernel.Sysctl = sysctl
	}

	if packages, ok, err := mapField(raw, "packages"); err != nil {
		return spec, err
	} else if ok {
		spec.Packages.Source = packages.Source
		install, err := packageItems(packages)
		if err != nil {
			return spec, err
		}
		spec.Packages.Install = install
	}

	if apt, ok, err := mapField(raw, "apt"); err != nil {
		return spec, err
	} else if ok {
		spec.APT.Source = apt.Source
		repositories, err := aptRepositorySpecs(apt)
		if err != nil {
			return spec, err
		}
		sourceFiles, err := aptSourceFileSpecs(apt)
		if err != nil {
			return spec, err
		}
		spec.APT.Repositories = repositories
		spec.APT.SourceFiles = sourceFiles
	}

	if files, ok, err := mapField(raw, "files"); err != nil {
		return spec, err
	} else if ok {
		spec.Files.Source = files.Source
		managedFiles, err := fileSpecs(files)
		if err != nil {
			return spec, err
		}
		if err := rejectHostFileOnChange(managedFiles); err != nil {
			return spec, err
		}
		spec.Files.Files = managedFiles
	}

	if secrets, ok, err := mapField(raw, "secrets"); err != nil {
		return spec, err
	} else if ok {
		spec.Secrets.Source = secrets.Source
		secretFiles, err := secretSpecs(secrets)
		if err != nil {
			return spec, err
		}
		spec.Secrets.Files = secretFiles
		c.warnSecretFilesDeprecated(secretFiles)
	}

	if directories, ok, err := mapField(raw, "directories"); err != nil {
		return spec, err
	} else if ok {
		spec.Directories.Source = directories.Source
		managedDirectories, err := directorySpecs(directories)
		if err != nil {
			return spec, err
		}
		spec.Directories.Directories = managedDirectories
	}

	if groups, ok, err := mapField(raw, "groups"); err != nil {
		return spec, err
	} else if ok {
		spec.Groups.Source = groups.Source
		managedGroups, err := groupSpecs(groups)
		if err != nil {
			return spec, err
		}
		spec.Groups.Groups = managedGroups
	}

	if users, ok, err := mapField(raw, "users"); err != nil {
		return spec, err
	} else if ok {
		spec.Users.Source = users.Source
		managedUsers, err := userSpecs(users)
		if err != nil {
			return spec, err
		}
		spec.Users.Users = managedUsers
	}

	if systemd, ok, err := mapField(raw, "systemd"); err != nil {
		return spec, err
	} else if ok {
		spec.Systemd.Source = systemd.Source
		units, err := systemdSpecs(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Units = units
		timers, err := systemdTimerSpecs(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Timers = timers
		networkd, err := networkdSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Networkd = networkd
		resolved, err := systemdResolvedSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Resolved = resolved
		journald, err := systemdJournaldSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Journald = journald
	}

	if services, ok, err := mapField(raw, "services"); err != nil {
		return spec, err
	} else if ok {
		spec.Services.Source = services.Source
		managedServices, err := serviceSpecs(services)
		if err != nil {
			return spec, err
		}
		spec.Services.Services = managedServices
	}

	if nftables, ok, err := mapField(raw, "nftables"); err != nil {
		return spec, err
	} else if ok {
		spec.Nftables.Source = nftables.Source
		compiled, err := nftablesSpec(nftables)
		if err != nil {
			return spec, err
		}
		spec.Nftables = compiled
	}

	if docker, ok, err := mapField(raw, "docker"); err != nil {
		return spec, err
	} else if ok {
		compiled, err := dockerSpec(docker)
		if err != nil {
			return spec, err
		}
		spec.Docker = compiled
	}

	return spec, nil
}

func applyHostFacts(spec *ir.HostSpec, facts ir.HostFacts) error {
	system := facts.System
	if system.Distribution != "" {
		if spec.Platform != nil && spec.Platform.Distribution != "" && spec.Platform.Distribution != system.Distribution {
			return fmt.Errorf("%s:%d:%s.distribution: declared platform.distribution %q does not match detected distribution %q", spec.Platform.Source.File, spec.Platform.Source.Line, spec.Platform.Source.Path, spec.Platform.Distribution, system.Distribution)
		}
		if spec.Platform == nil {
			spec.Platform = &ir.PlatformSpec{Source: spec.Source}
		}
		spec.Platform.Distribution = system.Distribution
	}
	if system.Version != "" {
		if spec.Platform != nil && spec.Platform.Version != "" && spec.Platform.Version != system.Version {
			return fmt.Errorf("%s:%d:%s.version: declared platform.version %q does not match detected version %q", spec.Platform.Source.File, spec.Platform.Source.Line, spec.Platform.Source.Path, spec.Platform.Version, system.Version)
		}
		if spec.Platform == nil {
			spec.Platform = &ir.PlatformSpec{Source: spec.Source}
		}
		spec.Platform.Version = system.Version
	}
	if system.Architecture != "" {
		if spec.Platform != nil && spec.Platform.Architecture != "" && spec.Platform.Architecture != system.Architecture {
			return fmt.Errorf("%s:%d:%s.architecture: declared platform.architecture %q does not match detected architecture %q", spec.Platform.Source.File, spec.Platform.Source.Line, spec.Platform.Source.Path, spec.Platform.Architecture, system.Architecture)
		}
		if spec.Platform == nil {
			spec.Platform = &ir.PlatformSpec{Source: spec.Source}
		}
		spec.Platform.Architecture = system.Architecture
	}
	if system.Codename != "" {
		if spec.Platform != nil && spec.Platform.Codename != "" && spec.Platform.Codename != system.Codename {
			return fmt.Errorf("%s:%d:%s.codename: declared platform.codename %q does not match detected codename %q", spec.Platform.Source.File, spec.Platform.Source.Line, spec.Platform.Source.Path, spec.Platform.Codename, system.Codename)
		}
		if spec.Platform == nil {
			spec.Platform = &ir.PlatformSpec{Source: spec.Source}
		}
		spec.Platform.Codename = system.Codename
	}
	if system.Distribution != "" && system.Version != "" && system.Architecture != "" && system.Codename != "" {
		if err := ir.ValidateTargetPlatform(system.Distribution, system.Version, system.Architecture, system.Codename); err != nil {
			return fmt.Errorf("host %q: %w", spec.Name, err)
		}
	}
	spec.Facts = facts
	return nil
}

func validateDeclaredPlatform(spec ir.PlatformSpec) error {
	source := spec.Source
	fieldError := func(field, format string, args ...any) error {
		return fmt.Errorf("%s:%d:%s.%s: %s", source.File, source.Line, source.Path, field, fmt.Sprintf(format, args...))
	}

	switch spec.Distribution {
	case "", "debian", "ubuntu":
	default:
		return fieldError("distribution", "unsupported platform.distribution %q", spec.Distribution)
	}
	if spec.Distribution == "ubuntu" {
		if spec.Version != "" && spec.Version != "24.04" && spec.Version != "26.04" {
			return fieldError("version", "unsupported Ubuntu platform.version %q; supported versions are 24.04 and 26.04", spec.Version)
		}
		if spec.Architecture != "" && spec.Architecture != "amd64" {
			return fieldError("architecture", "unsupported Ubuntu platform.architecture %q; only %q is supported", spec.Architecture, "amd64")
		}
		if spec.Codename != "" {
			switch spec.Version {
			case "24.04":
				if spec.Codename != "noble" {
					return fieldError("codename", "Ubuntu 24.04 platform.codename must be %q, got %q", "noble", spec.Codename)
				}
			case "26.04":
				if spec.Codename != "resolute" {
					return fieldError("codename", "Ubuntu 26.04 platform.codename must be %q, got %q", "resolute", spec.Codename)
				}
			case "":
				if spec.Codename != "noble" && spec.Codename != "resolute" {
					return fieldError("codename", "unsupported Ubuntu platform.codename %q; supported codenames are noble and resolute", spec.Codename)
				}
			}
		}
	}
	if spec.Distribution == "debian" {
		if spec.Version != "" && spec.Version != "12" && spec.Version != "13" {
			return fieldError("version", "unsupported Debian platform.version %q; supported versions are 12 and 13", spec.Version)
		}
		if spec.Architecture != "" && spec.Architecture != "amd64" && spec.Architecture != "arm64" {
			return fieldError("architecture", "unsupported Debian platform.architecture %q", spec.Architecture)
		}
		if spec.Version == "12" && spec.Codename != "" && spec.Codename != "bookworm" {
			return fieldError("codename", "Debian 12 platform.codename must be %q, got %q", "bookworm", spec.Codename)
		}
		if spec.Version == "13" && spec.Codename != "" && spec.Codename != "trixie" {
			return fieldError("codename", "Debian 13 platform.codename must be %q, got %q", "trixie", spec.Codename)
		}
	}
	if spec.Distribution != "" && spec.Version != "" && spec.Architecture != "" && spec.Codename != "" {
		if err := ir.ValidateTargetPlatform(spec.Distribution, spec.Version, spec.Architecture, spec.Codename); err != nil {
			return fieldError("distribution", "%v", err)
		}
	}
	return nil
}
