package termstyle

import "strings"

const (
	Reset   = "\x1b[0m"
	Bold    = "\x1b[1m"
	Dim     = "\x1b[2m"
	Black   = "\x1b[30m"
	Red     = "\x1b[31m"
	Green   = "\x1b[32m"
	Yellow  = "\x1b[33m"
	Blue    = "\x1b[34m"
	Magenta = "\x1b[35m"
	Cyan    = "\x1b[36m"
	White   = "\x1b[97m"
	Gray    = "\x1b[90m"

	BgRed     = "\x1b[41m"
	BgGreen   = "\x1b[42m"
	BgYellow  = "\x1b[43m"
	BgBlue    = "\x1b[44m"
	BgMagenta = "\x1b[45m"
	BgCyan    = "\x1b[46m"
	BgGray    = "\x1b[100m"
)

type Options struct {
	Color      bool
	Unicode    bool
	Background bool
}

func Apply(text string, opts Options, codes ...string) string {
	if !opts.Color || len(codes) == 0 {
		return text
	}
	return strings.Join(codes, "") + text + Reset
}

func ActionColor(action string) string {
	switch action {
	case "create", "adopt":
		return Green
	case "update":
		return Yellow
	case "delete", "destroy":
		return Red
	case "forget", "no-op":
		return Gray
	case "run":
		return Blue
	default:
		return ""
	}
}

func ActionBackground(action string) string {
	switch action {
	case "create", "adopt":
		return BgGreen
	case "update":
		return BgYellow
	case "delete", "destroy":
		return BgRed
	case "forget", "no-op":
		return BgGray
	case "run":
		return BgBlue
	default:
		return ""
	}
}

func DeleteBehaviorColor(behavior string) string {
	switch behavior {
	case "forget":
		return Gray
	case "remove-managed-artifact", "unknown":
		return Yellow
	case "restore-original":
		return Blue
	case "destructive":
		return Red
	case "external-side-effect":
		return Magenta
	default:
		return ""
	}
}

func DeleteBehaviorBackground(behavior string) string {
	switch behavior {
	case "forget":
		return BgGray
	case "remove-managed-artifact", "unknown":
		return BgYellow
	case "restore-original":
		return BgBlue
	case "destructive":
		return BgRed
	case "external-side-effect":
		return BgMagenta
	default:
		return ""
	}
}

func Badge(text string, opts Options, fg string, bg string) string {
	if !opts.Color {
		return text
	}
	if opts.Background && bg != "" {
		return Apply(" "+text+" ", opts, Bold, contrastForeground(bg), bg)
	}
	if fg != "" {
		return Apply(text, opts, Bold, fg)
	}
	return Apply(text, opts, Bold)
}

func contrastForeground(bg string) string {
	switch bg {
	case BgYellow, BgGreen, BgCyan:
		return Black
	default:
		return White
	}
}
