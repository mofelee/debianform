package ir

import "fmt"

const supportedTargetSummary = "Debian 12/13 on amd64/arm64 or Ubuntu 24.04 on amd64"

// ValidateTargetPlatform checks the complete identity discovered from a target host.
func ValidateTargetPlatform(distribution, version, architecture, codename string) error {
	switch distribution {
	case "debian":
		wantCodename := ""
		switch version {
		case "12":
			wantCodename = "bookworm"
		case "13":
			wantCodename = "trixie"
		default:
			return unsupportedTargetError(distribution, version, architecture, codename)
		}
		if architecture != "amd64" && architecture != "arm64" {
			return unsupportedTargetError(distribution, version, architecture, codename)
		}
		if codename != wantCodename {
			return fmt.Errorf("target platform distribution=%q version=%q reports codename %q, want %q", distribution, version, codename, wantCodename)
		}
	case "ubuntu":
		if version != "24.04" || architecture != "amd64" || codename != "noble" {
			return unsupportedTargetError(distribution, version, architecture, codename)
		}
	default:
		return unsupportedTargetError(distribution, version, architecture, codename)
	}
	return nil
}

func unsupportedTargetError(distribution, version, architecture, codename string) error {
	return fmt.Errorf(
		"unsupported target platform distribution=%q version=%q codename=%q architecture=%q; supported targets are %s",
		distribution,
		version,
		codename,
		architecture,
		supportedTargetSummary,
	)
}
