package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// These values are overridden with -ldflags for release builds.
var (
	Version = ""
	Commit  = "unknown"
	Date    = "unknown"
)

type Info struct {
	Version   string
	Commit    string
	Date      string
	Dirty     bool
	GoVersion string
	Platform  string
}

func Current() Info {
	info := Info{
		Version:   resolveVersion(Version),
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	build, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}

	if Version == "" && build.Main.Version != "" && build.Main.Version != "(devel)" {
		info.Version = build.Main.Version
	}

	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "unknown" {
				info.Commit = shortenCommit(setting.Value)
			}
		case "vcs.time":
			if info.Date == "unknown" {
				info.Date = setting.Value
			}
		case "vcs.modified":
			info.Dirty = setting.Value == "true"
		}
	}

	return info
}

func resolveVersion(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

func shortenCommit(commit string) string {
	const shortLength = 12
	if len(commit) <= shortLength {
		return commit
	}
	return commit[:shortLength]
}

func (i Info) Short() string {
	version := i.Version
	if i.Dirty && !strings.HasSuffix(version, "-dirty") && !strings.HasSuffix(version, "+dirty") {
		version += "-dirty"
	}
	return version
}
