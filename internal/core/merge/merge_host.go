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
		if value, ok, err := stringField(platform, "architecture"); err != nil {
			return spec, err
		} else if ok {
			spec.Platform.Architecture = value
		}
		if value, ok, err := stringField(platform, "codename"); err != nil {
			return spec, err
		} else if ok {
			spec.Platform.Codename = value
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
	spec.Facts = facts
	return nil
}
