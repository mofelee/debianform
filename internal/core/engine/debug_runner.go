package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/mofelee/debianform/internal/core/termstyle"
)

const (
	debugPreviewBytes = 8 * 1024
	debugPreviewLines = 200

	debugSyntaxFormatter = "terminal256"
	debugSyntaxStyle     = "github-dark"
)

var ErrDebugCancelled = errors.New("apply cancelled by debugger")

type DebugRunner struct {
	Inner   Runner
	Session *DebugSession
}

type DebugSession struct {
	w             io.Writer
	in            *bufio.Reader
	now           func() time.Time
	style         termstyle.Options
	next          int
	continueMode  bool
	nextRemaining int
	lastExec      string
	cancelled     bool
	current       debugCall
	lastResult    debugCallResult
}

type DebugSessionOptions struct {
	Writer io.Writer
	Input  io.Reader
	Now    func() time.Time
	Style  termstyle.Options
}

type debugCall struct {
	Index         int
	Host          string
	Context       RemoteCallContext
	Runner        string
	Script        string
	RemoteCommand string
	Stdin         []byte
}

type debugCallResult struct {
	Call   debugCall
	Result Result
	Err    error
	Valid  bool
}

func NewDebugSession(opts DebugSessionOptions) *DebugSession {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	var in *bufio.Reader
	if opts.Input != nil {
		in = bufio.NewReader(opts.Input)
	}
	return &DebugSession{w: opts.Writer, in: in, now: now, style: opts.Style}
}

func (r DebugRunner) Run(ctx context.Context, host, script string) (Result, error) {
	call := debugCall{Host: host, Runner: "Run", Script: script}
	return r.run(ctx, call, func() (Result, error) {
		if r.Inner == nil {
			return Result{}, fmt.Errorf("debug runner inner runner is required")
		}
		return r.Inner.Run(ctx, host, script)
	})
}

func (r DebugRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	payload, err := io.ReadAll(input)
	if err != nil {
		return Result{}, err
	}
	call := debugCall{Host: host, Runner: "RunInput", RemoteCommand: remoteCommand, Stdin: payload}
	return r.run(ctx, call, func() (Result, error) {
		if r.Inner == nil {
			return Result{}, fmt.Errorf("debug runner inner runner is required")
		}
		return r.Inner.RunInput(ctx, host, remoteCommand, bytes.NewReader(payload))
	})
}

func (r DebugRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	call := debugCall{Host: host, Runner: "RunCommand", RemoteCommand: remoteCommand}
	return r.run(ctx, call, func() (Result, error) {
		if r.Inner == nil {
			return Result{}, fmt.Errorf("debug runner inner runner is required")
		}
		return r.Inner.RunCommand(ctx, host, remoteCommand)
	})
}

func (r DebugRunner) run(ctx context.Context, call debugCall, fn func() (Result, error)) (Result, error) {
	session := r.Session
	if session == nil {
		return fn()
	}
	var err error
	if remoteContext, ok := RemoteCallContextFromContext(ctx); ok {
		call.Context = remoteContext
	}
	call, err = session.before(call)
	if err != nil {
		return Result{}, err
	}
	start := session.now()
	for {
		result, err := fn()
		session.after(call, result, err, session.now().Sub(start))
		retry, promptErr := session.afterFailure(call, err)
		if promptErr != nil {
			return result, promptErr
		}
		if !retry {
			return result, err
		}
		start = session.now()
		session.printRetry(call)
	}
}

func (s *DebugSession) before(call debugCall) (debugCall, error) {
	if s == nil {
		return call, nil
	}
	if s.cancelled && !call.Context.Cleanup {
		return call, ErrDebugCancelled
	}
	s.next++
	call.Index = s.next
	s.current = call
	if s.w == nil {
		return call, nil
	}
	s.printCall(call)
	if call.Context.Cleanup {
		return call, nil
	}
	if err := s.waitBeforeCall(call); err != nil {
		return call, err
	}
	return call, nil
}

func (s *DebugSession) printCall(call debugCall) {
	fmt.Fprintf(s.w, "%s #%d %s\n", s.debugPrefix(), call.Index, termstyle.Apply("before remote call", s.style, termstyle.Bold))
	s.printField("host", call.Host, termstyle.Cyan)
	phase := call.Context.Phase
	if phase == "" {
		phase = "remote call"
	}
	s.printField("phase", phase, termstyle.Cyan)
	if call.Context.Address != "" {
		s.printField("address", call.Context.Address, termstyle.Blue)
	}
	if call.Context.Action != "" {
		s.printField("action", call.Context.Action, termstyle.ActionColor(call.Context.Action))
	}
	if call.Context.Summary != "" {
		s.printField("summary", call.Context.Summary, termstyle.Gray)
	}
	if call.Context.Cleanup {
		s.printField("cleanup", "true", termstyle.Yellow)
	}
	s.printField("runner", call.Runner, termstyle.Magenta)
	if call.RemoteCommand != "" {
		writeDebugTextBlockWithStyle(s.w, "remote command", call.RemoteCommand, s.style, "bash")
	}
	if call.Script != "" {
		writeDebugTextBlockWithStyle(s.w, "script", call.Script, s.style, "bash")
	}
	if call.Stdin != nil {
		writeDebugBytesBlockWithStyle(s.w, "stdin", call.Stdin, s.style)
	}
}

func (s *DebugSession) after(call debugCall, result Result, err error, elapsed time.Duration) {
	if s == nil || s.w == nil {
		return
	}
	s.lastResult = debugCallResult{Call: call, Result: result, Err: err, Valid: true}
	status := "succeeded"
	if err != nil {
		status = "failed"
	}
	statusColor := termstyle.Green
	if err != nil {
		statusColor = termstyle.Red
	}
	fmt.Fprintf(s.w, "%s #%d remote call %s in %s\n", s.debugPrefix(), call.Index, termstyle.Apply(status, s.style, termstyle.Bold, statusColor), termstyle.Apply(formatDebugDuration(elapsed), s.style, termstyle.Gray))
	if err != nil {
		s.printField("error", err.Error(), termstyle.Red)
	}
	writeDebugBytesBlockWithStyle(s.w, "stdout", []byte(result.Stdout), s.style)
	writeDebugBytesBlockWithStyle(s.w, "stderr", []byte(result.Stderr), s.style)
}

func (s *DebugSession) afterFailure(call debugCall, err error) (bool, error) {
	if s == nil || s.in == nil || s.w == nil || err == nil || call.Context.Cleanup {
		return false, nil
	}
	if call.Context.onFailurePrompt != nil {
		call.Context.onFailurePrompt()
	}
	for {
		fmt.Fprint(s.w, termstyle.Apply("(dbfdbg failed) ", s.style, termstyle.Bold, termstyle.Red))
		line, readErr := s.in.ReadString('\n')
		if readErr != nil && !(errors.Is(readErr, io.EOF) && line != "") {
			if errors.Is(readErr, io.EOF) {
				s.cancelled = true
				return false, ErrDebugCancelled
			}
			return false, readErr
		}
		command := strings.TrimSpace(line)
		if command == "" {
			command = "show"
		}
		retry, done, err := s.handleFailedCommand(call, command)
		if err != nil {
			return false, err
		}
		if retry {
			return true, nil
		}
		if done {
			return false, nil
		}
	}
}

func (s *DebugSession) printRetry(call debugCall) {
	if s == nil || s.w == nil {
		return
	}
	fmt.Fprintf(s.w, "%s %s\n", s.debugPrefix(), termstyle.Apply(fmt.Sprintf("retrying #%d remote call", call.Index), s.style, termstyle.Bold, termstyle.Yellow))
	s.printCall(call)
}

func (s *DebugSession) waitBeforeCall(call debugCall) error {
	if s.in == nil {
		return nil
	}
	if s.continueMode {
		return nil
	}
	if s.nextRemaining > 0 {
		s.nextRemaining--
		return nil
	}
	for {
		fmt.Fprint(s.w, termstyle.Apply("(dbfdbg) ", s.style, termstyle.Bold, termstyle.Magenta))
		line, err := s.in.ReadString('\n')
		if err != nil && !(errors.Is(err, io.EOF) && line != "") {
			if errors.Is(err, io.EOF) {
				return ErrDebugCancelled
			}
			return err
		}
		command := strings.TrimSpace(line)
		if command == "" {
			command = s.lastExec
			if command == "" {
				command = "step"
			}
		}
		done, err := s.handleCommand(call, command)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (s *DebugSession) handleCommand(call debugCall, command string) (bool, error) {
	fields := strings.Fields(strings.ToLower(command))
	if len(fields) == 0 {
		return false, nil
	}
	switch fields[0] {
	case "s", "step":
		if len(fields) != 1 {
			s.printWarningf("step does not accept arguments")
			return false, nil
		}
		s.lastExec = "step"
		return true, nil
	case "n", "next":
		count := 1
		if len(fields) > 2 {
			s.printWarningf("usage: next [N]")
			return false, nil
		}
		if len(fields) == 2 {
			value, err := strconv.Atoi(fields[1])
			if err != nil || value < 1 {
				s.printWarningf("next requires a positive integer")
				return false, nil
			}
			count = value
		}
		s.nextRemaining = count - 1
		s.lastExec = fmt.Sprintf("next %d", count)
		return true, nil
	case "c", "continue":
		if len(fields) != 1 {
			s.printWarningf("continue does not accept arguments")
			return false, nil
		}
		s.continueMode = true
		s.lastExec = "continue"
		return true, nil
	case "l", "list", "show":
		if len(fields) == 1 {
			s.printCall(call)
			if fields[0] == "show" && s.lastResult.Valid {
				s.printResultDetails(s.lastResult)
			}
			return false, nil
		}
		if fields[0] == "show" && len(fields) == 2 {
			s.showPayload(call, fields[1])
			return false, nil
		}
		s.printWarningf("usage: show [stdin|stdout|stderr]")
		return false, nil
	case "q", "quit":
		if len(fields) != 1 {
			s.printWarningf("quit does not accept arguments")
			return false, nil
		}
		s.cancelled = true
		return false, ErrDebugCancelled
	case "h", "help":
		s.printHelp()
		return false, nil
	default:
		s.printWarningf("unknown command %q", command)
		s.printHelp()
		return false, nil
	}
}

func (s *DebugSession) handleFailedCommand(call debugCall, command string) (bool, bool, error) {
	fields := strings.Fields(strings.ToLower(command))
	if len(fields) == 0 {
		return false, false, nil
	}
	switch fields[0] {
	case "r", "retry":
		if len(fields) != 1 {
			s.printWarningf("retry does not accept arguments")
			return false, false, nil
		}
		return true, false, nil
	case "c", "continue":
		if len(fields) != 1 {
			s.printWarningf("continue does not accept arguments")
			return false, false, nil
		}
		s.lastExec = "continue"
		return false, true, nil
	case "l", "list", "show":
		if len(fields) == 1 {
			s.printCall(call)
			if s.lastResult.Valid {
				s.printResultDetails(s.lastResult)
			}
			return false, false, nil
		}
		if fields[0] == "show" && len(fields) == 2 {
			s.showPayload(call, fields[1])
			return false, false, nil
		}
		s.printWarningf("usage: show [stdin|stdout|stderr]")
		return false, false, nil
	case "s", "step", "n", "next":
		s.printWarningf("failed call cannot step or next; use retry, continue, or quit")
		return false, false, nil
	case "q", "quit":
		if len(fields) != 1 {
			s.printWarningf("quit does not accept arguments")
			return false, false, nil
		}
		s.cancelled = true
		return false, false, ErrDebugCancelled
	case "h", "help":
		s.printFailedHelp()
		return false, false, nil
	default:
		s.printWarningf("unknown failed command %q", command)
		s.printFailedHelp()
		return false, false, nil
	}
}

func (s *DebugSession) showPayload(call debugCall, name string) {
	switch name {
	case "stdin":
		if call.Stdin == nil {
			s.printWarningf("current call has no stdin")
			return
		}
		writeDebugPayloadFullWithStyle(s.w, "stdin", call.Stdin, s.style)
	case "stdout":
		if !s.lastResult.Valid {
			s.printWarningf("no previous stdout")
			return
		}
		writeDebugPayloadFullWithStyle(s.w, "stdout", []byte(s.lastResult.Result.Stdout), s.style)
	case "stderr":
		if !s.lastResult.Valid {
			s.printWarningf("no previous stderr")
			return
		}
		writeDebugPayloadFullWithStyle(s.w, "stderr", []byte(s.lastResult.Result.Stderr), s.style)
	default:
		s.printWarningf("usage: show [stdin|stdout|stderr]")
	}
}

func (s *DebugSession) printResultDetails(result debugCallResult) {
	writeDebugBytesBlockWithStyle(s.w, "stdout", []byte(result.Result.Stdout), s.style)
	writeDebugBytesBlockWithStyle(s.w, "stderr", []byte(result.Result.Stderr), s.style)
}

func (s *DebugSession) printHelp() {
	fmt.Fprintf(s.w, "%s %s\n", termstyle.Apply("dbf debugger commands:", s.style, termstyle.Bold, termstyle.Cyan), termstyle.Apply("step, next [N], continue, list, show [stdin|stdout|stderr], quit, help", s.style, termstyle.Gray))
}

func (s *DebugSession) printFailedHelp() {
	fmt.Fprintf(s.w, "%s %s\n", termstyle.Apply("dbf debugger failed commands:", s.style, termstyle.Bold, termstyle.Cyan), termstyle.Apply("show [stdin|stdout|stderr], retry, continue, quit, help", s.style, termstyle.Gray))
}

func writeDebugBytesBlock(w io.Writer, label string, data []byte) {
	writeDebugBytesBlockWithStyle(w, label, data, termstyle.Options{})
}

func writeDebugBytesBlockWithStyle(w io.Writer, label string, data []byte, style termstyle.Options) {
	if len(data) == 0 {
		fmt.Fprintf(w, "  %s: %s\n", debugBlockLabel(label, style), termstyle.Apply("<empty>", style, termstyle.Gray))
		return
	}
	payload := classifyDebugPayload(data)
	if payload.Binary {
		fmt.Fprintf(w, "  %s: %s\n", debugBlockLabel(label, style), termstyle.Apply(fmt.Sprintf("binary payload, length=%d, sha256=%s", len(data), payload.SHA256), style, termstyle.Yellow))
		writeDebugHint(w, fmt.Sprintf("type \"show %s\" to print a full hex dump", label), style)
		return
	}
	if payload.Long {
		fmt.Fprintf(w, "  %s: %s\n", debugBlockLabel(label, style), termstyle.Apply(fmt.Sprintf("text payload, length=%d, sha256=%s, %d lines", len(data), payload.SHA256, payload.Lines), style, termstyle.Gray))
		writeDebugTextBlockWithStyle(w, label+" preview", payload.Preview, style, debugPayloadLexer(payload.Preview))
		writeDebugHint(w, fmt.Sprintf("type \"show %s\" to print the full payload", label), style)
		return
	}
	fmt.Fprintf(w, "  %s: %s\n", debugBlockLabel(label, style), termstyle.Apply(fmt.Sprintf("text, length=%d, sha256=%s", len(data), payload.SHA256), style, termstyle.Gray))
	writeDebugTextBlockWithStyle(w, label, payload.Text, style, debugPayloadLexer(payload.Text))
}

func writeDebugTextBlock(w io.Writer, label string, value string) {
	writeDebugTextBlockWithStyle(w, label, value, termstyle.Options{}, "")
}

func writeDebugTextBlockWithStyle(w io.Writer, label string, value string, style termstyle.Options, lexerName string) {
	if value == "" {
		fmt.Fprintf(w, "  %s: %s\n", debugBlockLabel(label, style), termstyle.Apply("<empty>", style, termstyle.Gray))
		return
	}
	fmt.Fprintf(w, "  %s:\n", debugBlockLabel(label, style))
	fmt.Fprintf(w, "%s\n", termstyle.Apply(fmt.Sprintf("----- BEGIN %s -----", label), style, termstyle.Bold, termstyle.Gray))
	if !writeDebugHighlighted(w, value, style, lexerName) {
		fmt.Fprint(w, value)
	}
	if len(value) == 0 || value[len(value)-1] != '\n' {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%s\n", termstyle.Apply(fmt.Sprintf("----- END %s -----", label), style, termstyle.Bold, termstyle.Gray))
}

func formatDebugDuration(value time.Duration) string {
	if value < time.Second {
		return value.Round(time.Millisecond).String()
	}
	return value.Round(time.Second).String()
}

type debugPayload struct {
	Text    string
	Preview string
	SHA256  string
	Lines   int
	Long    bool
	Binary  bool
}

func classifyDebugPayload(data []byte) debugPayload {
	sum := sha256.Sum256(data)
	payload := debugPayload{
		SHA256: fmt.Sprintf("%x", sum[:]),
	}
	if !isDebugText(data) {
		payload.Binary = true
		return payload
	}
	text := string(data)
	payload.Text = text
	payload.Lines = debugLineCount(text)
	payload.Long = len(data) > debugPreviewBytes || payload.Lines > debugPreviewLines
	if payload.Long {
		payload.Preview = debugTextPreview(text)
	}
	return payload
}

func isDebugText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, r := range string(data) {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func debugLineCount(text string) int {
	if text == "" {
		return 0
	}
	lines := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		lines++
	}
	return lines
}

func debugTextPreview(text string) string {
	var b strings.Builder
	bytesWritten := 0
	linesWritten := 0
	for _, r := range text {
		size := len(string(r))
		if bytesWritten+size > debugPreviewBytes || linesWritten >= debugPreviewLines {
			break
		}
		b.WriteRune(r)
		bytesWritten += size
		if r == '\n' {
			linesWritten++
		}
	}
	return b.String()
}

func writeDebugPayloadFull(w io.Writer, label string, data []byte) {
	writeDebugPayloadFullWithStyle(w, label, data, termstyle.Options{})
}

func writeDebugPayloadFullWithStyle(w io.Writer, label string, data []byte, style termstyle.Options) {
	if len(data) == 0 {
		fmt.Fprintf(w, "  %s: %s\n", debugBlockLabel(label, style), termstyle.Apply("<empty>", style, termstyle.Gray))
		return
	}
	payload := classifyDebugPayload(data)
	if payload.Binary {
		fmt.Fprintf(w, "%s\n", termstyle.Apply(fmt.Sprintf("----- BEGIN %s hex -----", label), style, termstyle.Bold, termstyle.Gray))
		fmt.Fprint(w, termstyle.Apply(hex.Dump(data), style, termstyle.Yellow))
		fmt.Fprintf(w, "%s\n", termstyle.Apply(fmt.Sprintf("----- END %s hex -----", label), style, termstyle.Bold, termstyle.Gray))
		return
	}
	writeDebugTextBlockWithStyle(w, label, payload.Text, style, debugPayloadLexer(payload.Text))
}

func (s *DebugSession) debugPrefix() string {
	return termstyle.Apply("dbf debugger:", s.style, termstyle.Bold, termstyle.Cyan)
}

func (s *DebugSession) printField(label string, value string, codes ...string) {
	fmt.Fprintf(s.w, "  %s: %s\n", debugBlockLabel(label, s.style), debugApply(value, s.style, codes...))
}

func (s *DebugSession) printWarningf(format string, args ...any) {
	fmt.Fprintf(s.w, "%s %s\n", s.debugPrefix(), termstyle.Apply(fmt.Sprintf(format, args...), s.style, termstyle.Yellow))
}

func debugBlockLabel(label string, style termstyle.Options) string {
	return termstyle.Apply(label, style, termstyle.Bold, termstyle.Gray)
}

func debugApply(text string, style termstyle.Options, codes ...string) string {
	filtered := codes[:0]
	for _, code := range codes {
		if code != "" {
			filtered = append(filtered, code)
		}
	}
	return termstyle.Apply(text, style, filtered...)
}

func writeDebugHint(w io.Writer, text string, style termstyle.Options) {
	fmt.Fprintf(w, "  %s: %s\n", debugBlockLabel("hint", style), termstyle.Apply(text, style, termstyle.Yellow))
}

func debugPayloadLexer(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if json.Valid([]byte(trimmed)) {
		return "json"
	}
	if strings.HasPrefix(trimmed, "#!") {
		return "bash"
	}
	return ""
}

func writeDebugHighlighted(w io.Writer, value string, style termstyle.Options, lexerName string) bool {
	if !style.Color || lexerName == "" {
		return false
	}
	lexer := lexers.Get(lexerName)
	if lexer == nil {
		return false
	}
	formatter := formatters.Get(debugSyntaxFormatter)
	if formatter == nil {
		return false
	}
	iterator, err := lexer.Tokenise(nil, value)
	if err != nil {
		return false
	}
	return formatter.Format(w, styles.Get(debugSyntaxStyle), iterator) == nil
}
