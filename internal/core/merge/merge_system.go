package merge

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

var (
	modePattern                   = regexp.MustCompile(`^0[0-7]{3}$`)
	sha256Pattern                 = regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)
	systemLocalePattern           = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_-]+)?(@[A-Za-z0-9_-]+)?$`)
	systemTimezoneTokenPattern    = regexp.MustCompile(`^[A-Za-z0-9_+./-]+$`)
	systemdEnvironmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	systemdSafeTokenPattern       = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)
)

func validateSystemTimezone(value string, source ir.SourceRef) error {
	if value == "" {
		return fmt.Errorf("%s:%d:%s: system.timezone must be non-empty", source.File, source.Line, source.Path)
	}
	if filepath.IsAbs(value) || strings.Contains(value, "\\") || strings.ContainsAny(value, " \t\r\n") || !systemTimezoneTokenPattern.MatchString(value) {
		return fmt.Errorf("%s:%d:%s: system.timezone must be a zoneinfo name such as \"UTC\" or \"Asia/Shanghai\"", source.File, source.Line, source.Path)
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("%s:%d:%s: system.timezone must not contain empty, current, or parent path segments", source.File, source.Line, source.Path)
		}
	}
	return nil
}

func validateSystemLocale(value string, source ir.SourceRef) error {
	if value == "" {
		return fmt.Errorf("%s:%d:%s: system.locale must be non-empty", source.File, source.Line, source.Path)
	}
	if strings.ContainsAny(value, " \t\r\n'\"`$;&|<>\\") || !systemLocalePattern.MatchString(value) {
		return fmt.Errorf("%s:%d:%s: system.locale must be a locale name such as \"en_US.UTF-8\", \"C\", or \"C.UTF-8\"", source.File, source.Line, source.Path)
	}
	return nil
}

func validServiceState(state string) bool {
	switch state {
	case "running", "stopped", "restarted", "reloaded":
		return true
	default:
		return false
	}
}

func serviceUnitName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".service"
}

func timerUnitName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".timer"
}
