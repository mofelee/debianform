package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	debugPreviewBytes = 8 * 1024
	debugPreviewLines = 200
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
	return &DebugSession{w: opts.Writer, in: in, now: now}
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
	fmt.Fprintf(s.w, "dbf debugger: #%d before remote call\n", call.Index)
	fmt.Fprintf(s.w, "  host: %s\n", call.Host)
	phase := call.Context.Phase
	if phase == "" {
		phase = "remote call"
	}
	fmt.Fprintf(s.w, "  phase: %s\n", phase)
	if call.Context.Address != "" {
		fmt.Fprintf(s.w, "  address: %s\n", call.Context.Address)
	}
	if call.Context.Action != "" {
		fmt.Fprintf(s.w, "  action: %s\n", call.Context.Action)
	}
	if call.Context.Summary != "" {
		fmt.Fprintf(s.w, "  summary: %s\n", call.Context.Summary)
	}
	if call.Context.Cleanup {
		fmt.Fprintln(s.w, "  cleanup: true")
	}
	fmt.Fprintf(s.w, "  runner: %s\n", call.Runner)
	if call.RemoteCommand != "" {
		writeDebugTextBlock(s.w, "remote command", call.RemoteCommand)
	}
	if call.Script != "" {
		writeDebugTextBlock(s.w, "script", call.Script)
	}
	if call.Stdin != nil {
		writeDebugBytesBlock(s.w, "stdin", call.Stdin)
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
	fmt.Fprintf(s.w, "dbf debugger: #%d remote call %s in %s\n", call.Index, status, formatDebugDuration(elapsed))
	if err != nil {
		fmt.Fprintf(s.w, "  error: %v\n", err)
	}
	writeDebugBytesBlock(s.w, "stdout", []byte(result.Stdout))
	writeDebugBytesBlock(s.w, "stderr", []byte(result.Stderr))
}

func (s *DebugSession) afterFailure(call debugCall, err error) (bool, error) {
	if s == nil || s.in == nil || s.w == nil || err == nil || call.Context.Cleanup {
		return false, nil
	}
	for {
		fmt.Fprint(s.w, "(dbfdbg failed) ")
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
		retry, err := s.handleFailedCommand(call, command)
		if err != nil {
			return false, err
		}
		if retry {
			return true, nil
		}
	}
}

func (s *DebugSession) printRetry(call debugCall) {
	if s == nil || s.w == nil {
		return
	}
	fmt.Fprintf(s.w, "dbf debugger: retrying #%d remote call\n", call.Index)
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
		fmt.Fprint(s.w, "(dbfdbg) ")
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
			fmt.Fprintln(s.w, "dbf debugger: step does not accept arguments")
			return false, nil
		}
		s.lastExec = "step"
		return true, nil
	case "n", "next":
		count := 1
		if len(fields) > 2 {
			fmt.Fprintln(s.w, "dbf debugger: usage: next [N]")
			return false, nil
		}
		if len(fields) == 2 {
			value, err := strconv.Atoi(fields[1])
			if err != nil || value < 1 {
				fmt.Fprintln(s.w, "dbf debugger: next requires a positive integer")
				return false, nil
			}
			count = value
		}
		s.nextRemaining = count - 1
		s.lastExec = fmt.Sprintf("next %d", count)
		return true, nil
	case "c", "continue":
		if len(fields) != 1 {
			fmt.Fprintln(s.w, "dbf debugger: continue does not accept arguments")
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
		fmt.Fprintln(s.w, "dbf debugger: usage: show [stdin|stdout|stderr]")
		return false, nil
	case "q", "quit":
		if len(fields) != 1 {
			fmt.Fprintln(s.w, "dbf debugger: quit does not accept arguments")
			return false, nil
		}
		s.cancelled = true
		return false, ErrDebugCancelled
	case "h", "help":
		s.printHelp()
		return false, nil
	default:
		fmt.Fprintf(s.w, "dbf debugger: unknown command %q\n", command)
		s.printHelp()
		return false, nil
	}
}

func (s *DebugSession) handleFailedCommand(call debugCall, command string) (bool, error) {
	fields := strings.Fields(strings.ToLower(command))
	if len(fields) == 0 {
		return false, nil
	}
	switch fields[0] {
	case "r", "retry":
		if len(fields) != 1 {
			fmt.Fprintln(s.w, "dbf debugger: retry does not accept arguments")
			return false, nil
		}
		return true, nil
	case "l", "list", "show":
		if len(fields) == 1 {
			s.printCall(call)
			if s.lastResult.Valid {
				s.printResultDetails(s.lastResult)
			}
			return false, nil
		}
		if fields[0] == "show" && len(fields) == 2 {
			s.showPayload(call, fields[1])
			return false, nil
		}
		fmt.Fprintln(s.w, "dbf debugger: usage: show [stdin|stdout|stderr]")
		return false, nil
	case "s", "step", "n", "next", "c", "continue":
		fmt.Fprintln(s.w, "dbf debugger: failed call cannot step, next, or continue; use retry or quit")
		return false, nil
	case "q", "quit":
		if len(fields) != 1 {
			fmt.Fprintln(s.w, "dbf debugger: quit does not accept arguments")
			return false, nil
		}
		s.cancelled = true
		return false, ErrDebugCancelled
	case "h", "help":
		s.printFailedHelp()
		return false, nil
	default:
		fmt.Fprintf(s.w, "dbf debugger: unknown failed command %q\n", command)
		s.printFailedHelp()
		return false, nil
	}
}

func (s *DebugSession) showPayload(call debugCall, name string) {
	switch name {
	case "stdin":
		if call.Stdin == nil {
			fmt.Fprintln(s.w, "dbf debugger: current call has no stdin")
			return
		}
		writeDebugPayloadFull(s.w, "stdin", call.Stdin)
	case "stdout":
		if !s.lastResult.Valid {
			fmt.Fprintln(s.w, "dbf debugger: no previous stdout")
			return
		}
		writeDebugPayloadFull(s.w, "stdout", []byte(s.lastResult.Result.Stdout))
	case "stderr":
		if !s.lastResult.Valid {
			fmt.Fprintln(s.w, "dbf debugger: no previous stderr")
			return
		}
		writeDebugPayloadFull(s.w, "stderr", []byte(s.lastResult.Result.Stderr))
	default:
		fmt.Fprintln(s.w, "dbf debugger: usage: show [stdin|stdout|stderr]")
	}
}

func (s *DebugSession) printResultDetails(result debugCallResult) {
	writeDebugBytesBlock(s.w, "stdout", []byte(result.Result.Stdout))
	writeDebugBytesBlock(s.w, "stderr", []byte(result.Result.Stderr))
}

func (s *DebugSession) printHelp() {
	fmt.Fprintln(s.w, "dbf debugger commands: step, next [N], continue, list, show [stdin|stdout|stderr], quit, help")
}

func (s *DebugSession) printFailedHelp() {
	fmt.Fprintln(s.w, "dbf debugger failed commands: show [stdin|stdout|stderr], retry, quit, help")
}

func writeDebugBytesBlock(w io.Writer, label string, data []byte) {
	if len(data) == 0 {
		fmt.Fprintf(w, "  %s: <empty>\n", label)
		return
	}
	payload := classifyDebugPayload(data)
	if payload.Binary {
		fmt.Fprintf(w, "  %s: binary payload, length=%d, sha256=%s\n", label, len(data), payload.SHA256)
		fmt.Fprintf(w, "  hint: type \"show %s\" to print a full hex dump\n", label)
		return
	}
	if payload.Long {
		fmt.Fprintf(w, "  %s: text payload, length=%d, sha256=%s, %d lines\n", label, len(data), payload.SHA256, payload.Lines)
		writeDebugTextBlock(w, label+" preview", payload.Preview)
		fmt.Fprintf(w, "  hint: type \"show %s\" to print the full payload\n", label)
		return
	}
	fmt.Fprintf(w, "  %s: text, length=%d, sha256=%s\n", label, len(data), payload.SHA256)
	writeDebugTextBlock(w, label, payload.Text)
}

func writeDebugTextBlock(w io.Writer, label string, value string) {
	if value == "" {
		fmt.Fprintf(w, "  %s: <empty>\n", label)
		return
	}
	fmt.Fprintf(w, "  %s:\n", label)
	fmt.Fprintf(w, "----- BEGIN %s -----\n", label)
	fmt.Fprint(w, value)
	if len(value) == 0 || value[len(value)-1] != '\n' {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "----- END %s -----\n", label)
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
	if len(data) == 0 {
		fmt.Fprintf(w, "  %s: <empty>\n", label)
		return
	}
	payload := classifyDebugPayload(data)
	if payload.Binary {
		fmt.Fprintf(w, "----- BEGIN %s hex -----\n", label)
		fmt.Fprint(w, hex.Dump(data))
		fmt.Fprintf(w, "----- END %s hex -----\n", label)
		return
	}
	writeDebugTextBlock(w, label, payload.Text)
}
