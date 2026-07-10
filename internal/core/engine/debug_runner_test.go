package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
	"github.com/mofelee/debianform/internal/core/termstyle"
)

func TestDebugRunnerRunRecordsAndForwards(t *testing.T) {
	inner := &debugRecordingRunner{result: Result{Stdout: "ok\n", Stderr: "warn\n"}}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), 15*time.Millisecond),
		}),
	}

	result, err := runner.Run(context.Background(), "server1", "echo ok\n")
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "ok\n" || result.Stderr != "warn\n" {
		t.Fatalf("result = %#v", result)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %#v, want one", inner.calls)
	}
	call := inner.calls[0]
	if call.runner != "Run" || call.host != "server1" || call.script != "echo ok\n" {
		t.Fatalf("inner call = %#v", call)
	}
	text := out.String()
	for _, want := range []string{
		"dbf debugger: #1 before remote call",
		"host: server1",
		"runner: Run",
		"----- BEGIN script -----\necho ok\n----- END script -----",
		"dbf debugger: #1 remote call succeeded in 15ms",
		"----- BEGIN stdout -----\nok\n----- END stdout -----",
		"----- BEGIN stderr -----\nwarn\n----- END stderr -----",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugRunnerColorizesDebuggerAndScriptOutput(t *testing.T) {
	inner := &debugRecordingRunner{result: Result{Stdout: "{\"ok\":true}\n"}}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
			Style:  termstyle.Options{Color: true, Background: true},
		}),
	}

	_, err := runner.Run(context.Background(), "server1", "if [ -n \"$HOME\" ]; then\n  echo ok\nfi\n")
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"\x1b[1m\x1b[36mdbf debugger:\x1b[0m",
		"\x1b[1m\x1b[32msucceeded\x1b[0m",
		"\x1b[38;5;",
		"----- BEGIN script -----",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("colored debug output missing %q:\n%q", want, text)
		}
	}
}

func TestDebugRunnerRunInputRecordsPayloadAndForwards(t *testing.T) {
	inner := &debugRecordingRunner{result: Result{}}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	_, err := runner.RunInput(context.Background(), "server1", "cat > /tmp/file", strings.NewReader("payload\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %#v, want one", inner.calls)
	}
	call := inner.calls[0]
	if call.runner != "RunInput" || call.host != "server1" || call.remoteCommand != "cat > /tmp/file" || call.input != "payload\n" {
		t.Fatalf("inner call = %#v", call)
	}
	text := out.String()
	for _, want := range []string{
		"runner: RunInput",
		"----- BEGIN remote command -----\ncat > /tmp/file\n----- END remote command -----",
		"----- BEGIN stdin -----\npayload\n----- END stdin -----",
		"stdout: <empty>",
		"stderr: <empty>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugRunnerRunCommandRecordsAndForwards(t *testing.T) {
	inner := &debugRecordingRunner{result: Result{Stdout: "active\n"}}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), 2*time.Millisecond),
		}),
	}

	_, err := runner.RunCommand(context.Background(), "server1", "systemctl is-active app")
	if err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %#v, want one", inner.calls)
	}
	call := inner.calls[0]
	if call.runner != "RunCommand" || call.remoteCommand != "systemctl is-active app" {
		t.Fatalf("inner call = %#v", call)
	}
	text := out.String()
	for _, want := range []string{
		"runner: RunCommand",
		"----- BEGIN remote command -----\nsystemctl is-active app\n----- END remote command -----",
		"dbf debugger: #1 remote call succeeded in 2ms",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugRunnerPrintsRemoteCallContext(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}
	ctx := WithRemoteCallContext(context.Background(), RemoteCallContext{
		Phase:   "apply resource",
		Address: `host.server1.packages.package["curl"]`,
		Action:  ActionCreate,
		Summary: "install package curl",
	})

	if _, err := runner.Run(ctx, "server1", "true\n"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"phase: apply resource",
		`address: host.server1.packages.package["curl"]`,
		"action: create",
		"summary: install package curl",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugRunnerPrintsFallbackRemoteCallPhase(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.Run(context.Background(), "server1", "true\n"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "phase: remote call") {
		t.Fatalf("debug output missing fallback phase:\n%s", text)
	}
}

func TestDiscoverHostFactsAddsRemoteCallContext(t *testing.T) {
	inner := &debugRecordingRunner{result: Result{Stdout: "hostname=server1\narchitecture=amd64\ncodename=trixie\n"}}

	_, err := DiscoverHostFacts(context.Background(), inner, ir.HostSpec{Name: "server1"}, func() time.Time {
		return time.Unix(0, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inner.contexts) != 1 {
		t.Fatalf("contexts = %#v, want one", inner.contexts)
	}
	got := inner.contexts[0]
	if got.Phase != "discover facts" || got.Action != "inspect" || got.Summary != "system facts" {
		t.Fatalf("remote call context = %#v", got)
	}
}

func TestSSHBackendAddsStateRemoteCallContexts(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}
	backend := SSHBackend{Runner: runner, Warnings: io.Discard}
	host := ir.HostSpec{
		Name: "server1",
		State: ir.StateSpec{
			Path:     "/var/lib/dbf/server1.json",
			LockPath: "/var/lib/dbf/server1.lock",
		},
	}

	if _, err := backend.Read(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Write(context.Background(), host, corestate.Empty("server1")); err != nil {
		t.Fatal(err)
	}
	lock, err := backend.Lock(context.Background(), host, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	for _, want := range []string{
		"phase: state read",
		"phase: state write",
		"phase: state lock",
		"phase: state unlock",
		"cleanup: true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestEngineApplyAddsResourceAndOperationRemoteCallContexts(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	node := graph.Node{
		Host:    "server1",
		Address: `host.server1.files.file["/tmp/app.conf"]`,
		Kind:    "file",
		Summary: "create file /tmp/app.conf",
		Desired: map[string]any{
			"path":   "/tmp/app.conf",
			"ensure": "present",
		},
	}
	operation := graph.Operation{
		Host:        "server1",
		Address:     "host.server1.operations.reload",
		Action:      ActionRun,
		Summary:     "reload app",
		TriggeredBy: []string{node.Address},
	}
	provider := &contextRecordingProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{Nodes: []graph.Node{node}, Operations: []graph.Operation{operation}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(provider.applyContexts) != 1 {
		t.Fatalf("apply contexts = %#v, want one", provider.applyContexts)
	}
	applyContext := provider.applyContexts[0]
	if applyContext.Phase != "apply resource" || applyContext.Address != node.Address || applyContext.Action != ActionCreate || applyContext.Summary != node.Summary {
		t.Fatalf("apply context = %#v", applyContext)
	}
	if len(provider.operationContexts) != 1 {
		t.Fatalf("operation contexts = %#v, want one", provider.operationContexts)
	}
	operationContext := provider.operationContexts[0]
	if operationContext.Phase != "run operation" || operationContext.Address != operation.Address || operationContext.Action != ActionRun || operationContext.Summary != operation.Summary {
		t.Fatalf("operation context = %#v", operationContext)
	}
}

func TestDebugRunnerRecordsFailures(t *testing.T) {
	inner := &debugRecordingRunner{
		result: Result{Stdout: "partial\n", Stderr: "bad\n"},
		err:    fmt.Errorf("injected failure"),
	}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), 3*time.Millisecond),
		}),
	}

	result, err := runner.Run(context.Background(), "server1", "false\n")
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("error = %v, want injected failure", err)
	}
	if result.Stdout != "partial\n" || result.Stderr != "bad\n" {
		t.Fatalf("result = %#v", result)
	}
	text := out.String()
	for _, want := range []string{
		"dbf debugger: #1 remote call failed in 3ms",
		"error: injected failure",
		"----- BEGIN stdout -----\npartial\n----- END stdout -----",
		"----- BEGIN stderr -----\nbad\n----- END stderr -----",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugPayloadShortTextRendersFullContent(t *testing.T) {
	var out bytes.Buffer
	writeDebugBytesBlock(&out, "stdin", []byte("hello\n"))
	text := out.String()
	for _, want := range []string{
		"stdin: text, length=6, sha256=",
		"----- BEGIN stdin -----\nhello\n----- END stdin -----",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("short text output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugPayloadLongTextRendersSummaryAndPreview(t *testing.T) {
	longText := strings.Repeat("line\n", debugPreviewLines+5)
	var out bytes.Buffer
	writeDebugBytesBlock(&out, "stdin", []byte(longText))
	text := out.String()
	for _, want := range []string{
		"stdin: text payload, length=",
		"sha256=",
		"205 lines",
		"----- BEGIN stdin preview -----",
		"hint: type \"show stdin\" to print the full payload",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("long text output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, strings.Repeat("line\n", debugPreviewLines+1)) {
		t.Fatalf("long text output rendered more than preview:\n%s", text)
	}
}

func TestDebugPayloadBinaryRendersSummaryAndFullHexDump(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}
	var summary bytes.Buffer
	writeDebugBytesBlock(&summary, "stdin", data)
	summaryText := summary.String()
	for _, want := range []string{
		"stdin: binary payload, length=6, sha256=",
		"hint: type \"show stdin\" to print a full hex dump",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("binary summary missing %q:\n%s", want, summaryText)
		}
	}
	if strings.Contains(summaryText, "BEGIN stdin hex") {
		t.Fatalf("binary summary unexpectedly rendered hex dump:\n%s", summaryText)
	}

	var full bytes.Buffer
	writeDebugPayloadFull(&full, "stdin", data)
	fullText := full.String()
	for _, want := range []string{
		"----- BEGIN stdin hex -----",
		"00000000  89 50 4e 47 00 01",
		"----- END stdin hex -----",
	} {
		if !strings.Contains(fullText, want) {
			t.Fatalf("binary full output missing %q:\n%s", want, fullText)
		}
	}
}

func TestDebugRunnerUsesPayloadStrategyForStdoutAndStderr(t *testing.T) {
	inner := &debugRecordingRunner{
		result: Result{
			Stdout: strings.Repeat("out\n", debugPreviewLines+1),
			Stderr: string([]byte{
				0x01, 0x02, 'e', 'r', 'r',
			}),
		},
	}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.Run(context.Background(), "server1", "echo\n"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"stdout: text payload, length=",
		"hint: type \"show stdout\" to print the full payload",
		"stderr: binary payload, length=5, sha256=",
		"hint: type \"show stderr\" to print a full hex dump",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugRunnerInteractiveStepExecutesOneCall(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("\nq\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.Run(context.Background(), "server1", "one\n"); err != nil {
		t.Fatal(err)
	}
	_, err := runner.Run(context.Background(), "server1", "two\n")
	if !errors.Is(err, ErrDebugCancelled) {
		t.Fatalf("second run error = %v, want ErrDebugCancelled", err)
	}
	if len(inner.calls) != 1 || inner.calls[0].script != "one\n" {
		t.Fatalf("inner calls = %#v, want only first call", inner.calls)
	}
	if !strings.Contains(out.String(), "(dbfdbg)") {
		t.Fatalf("interactive output missing prompt:\n%s", out.String())
	}
}

func TestDebugRunnerInteractiveNextExecutesCount(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("next 5\nq\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	for i := 0; i < 5; i++ {
		if _, err := runner.Run(context.Background(), "server1", fmt.Sprintf("call-%d\n", i)); err != nil {
			t.Fatalf("run %d failed: %v", i, err)
		}
	}
	_, err := runner.Run(context.Background(), "server1", "call-5\n")
	if !errors.Is(err, ErrDebugCancelled) {
		t.Fatalf("sixth run error = %v, want ErrDebugCancelled", err)
	}
	if len(inner.calls) != 5 {
		t.Fatalf("inner calls = %d, want 5", len(inner.calls))
	}
	if prompts := strings.Count(out.String(), "(dbfdbg)"); prompts != 2 {
		t.Fatalf("prompt count = %d, want 2:\n%s", prompts, out.String())
	}
}

func TestDebugRunnerInteractiveContinueDoesNotPromptAgain(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("continue\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	for i := 0; i < 3; i++ {
		if _, err := runner.Run(context.Background(), "server1", fmt.Sprintf("call-%d\n", i)); err != nil {
			t.Fatalf("run %d failed: %v", i, err)
		}
	}
	if len(inner.calls) != 3 {
		t.Fatalf("inner calls = %d, want 3", len(inner.calls))
	}
	if prompts := strings.Count(out.String(), "(dbfdbg)"); prompts != 1 {
		t.Fatalf("prompt count = %d, want 1:\n%s", prompts, out.String())
	}
}

func TestDebugRunnerInteractiveShowStdinDoesNotExecute(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("show stdin\nstep\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.RunInput(context.Background(), "server1", "cat", strings.NewReader("payload\n")); err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %d, want 1", len(inner.calls))
	}
	text := out.String()
	if strings.Count(text, "----- BEGIN stdin -----") < 2 {
		t.Fatalf("show stdin did not re-render stdin:\n%s", text)
	}
}

func TestDebugRunnerInteractiveShowPreviousStdoutAndStderr(t *testing.T) {
	inner := &debugRecordingRunner{result: Result{Stdout: "out\n", Stderr: "err\n"}}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("step\nshow stdout\nshow stderr\nquit\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.Run(context.Background(), "server1", "one\n"); err != nil {
		t.Fatal(err)
	}
	_, err := runner.Run(context.Background(), "server1", "two\n")
	if !errors.Is(err, ErrDebugCancelled) {
		t.Fatalf("second run error = %v, want ErrDebugCancelled", err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %d, want 1", len(inner.calls))
	}
	text := out.String()
	if strings.Count(text, "----- BEGIN stdout -----\nout\n----- END stdout -----") < 2 {
		t.Fatalf("show stdout did not render previous stdout:\n%s", text)
	}
	if strings.Count(text, "----- BEGIN stderr -----\nerr\n----- END stderr -----") < 2 {
		t.Fatalf("show stderr did not render previous stderr:\n%s", text)
	}
}

func TestDebugRunnerInteractiveUnknownCommandDoesNotExecute(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("wat\nstep\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.Run(context.Background(), "server1", "one\n"); err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %d, want 1", len(inner.calls))
	}
	text := out.String()
	if !strings.Contains(text, `dbf debugger: unknown command "wat"`) ||
		!strings.Contains(text, "dbf debugger commands:") {
		t.Fatalf("unknown command output missing help:\n%s", text)
	}
}

func TestDebugRunnerFailedPromptRetrySucceeds(t *testing.T) {
	inner := &debugSequenceRunner{
		results: []Result{
			{Stdout: "partial\n", Stderr: "failed\n"},
			{Stdout: "ok\n"},
		},
		errs: []error{fmt.Errorf("injected failure"), nil},
	}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("step\nretry\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	result, err := runner.Run(context.Background(), "server1", "sometimes\n")
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "ok\n" {
		t.Fatalf("result = %#v, want retry success", result)
	}
	if len(inner.calls) != 2 {
		t.Fatalf("inner calls = %#v, want initial call and retry", inner.calls)
	}
	text := out.String()
	for _, want := range []string{
		"(dbfdbg failed)",
		"dbf debugger: retrying #1 remote call",
		"dbf debugger: #1 remote call succeeded",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugRunnerFailedPromptStopsProgressHeartbeat(t *testing.T) {
	inner := &debugSequenceRunner{
		errs: []error{fmt.Errorf("injected failure"), nil},
	}
	var out bytes.Buffer
	stopped := 0
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("step\nretry\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}
	ctx := WithRemoteCallContext(context.Background(), RemoteCallContext{
		onFailurePrompt: func() {
			stopped++
		},
	})

	if _, err := runner.Run(ctx, "server1", "sometimes\n"); err != nil {
		t.Fatal(err)
	}
	if stopped != 1 {
		t.Fatalf("failure prompt callback count = %d, want 1", stopped)
	}
}

func TestDebugRunnerRetryUsesSameStdinPayload(t *testing.T) {
	inner := &debugSequenceRunner{
		errs: []error{fmt.Errorf("injected failure"), nil},
	}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("step\nretry\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.RunInput(context.Background(), "server1", "cat > /tmp/file", strings.NewReader("payload\n")); err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 2 {
		t.Fatalf("inner calls = %#v, want initial call and retry", inner.calls)
	}
	for _, call := range inner.calls {
		if call.runner != "RunInput" || call.input != "payload\n" {
			t.Fatalf("retry call = %#v, want same stdin payload", call)
		}
	}
}

func TestDebugRunnerFailedPromptContinueReturnsOriginalError(t *testing.T) {
	inner := &debugSequenceRunner{
		errs: []error{fmt.Errorf("injected failure"), nil},
	}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("step\nstep\nnext 2\ncontinue\nretry\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.Run(context.Background(), "server1", "sometimes\n"); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("continue after failed prompt error = %v, want original failure", err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %#v, want only initial call", inner.calls)
	}
	text := out.String()
	if count := strings.Count(text, "failed call cannot step or next"); count != 2 {
		t.Fatalf("failed prompt rejection count = %d, want 2:\n%s", count, text)
	}
	if strings.Contains(text, "retrying #1 remote call") {
		t.Fatalf("continue unexpectedly retried failed call:\n%s", text)
	}
}

func TestDebugRunnerFailedPromptRunsDiagnosticCommand(t *testing.T) {
	inner := &debugSequenceRunner{
		results: []Result{
			{},
			{Stdout: "status output\n", Stderr: "journal hint\n"},
		},
		errs: []error{fmt.Errorf("injected failure"), nil},
	}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("step\nrun systemctl status app\ncontinue\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	if _, err := runner.Run(context.Background(), "server1", "sometimes\n"); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("run after diagnostic error = %v, want original failure", err)
	}
	if len(inner.calls) != 2 {
		t.Fatalf("inner calls = %#v, want original run and diagnostic command", inner.calls)
	}
	if inner.calls[1].runner != "RunCommand" || inner.calls[1].host != "server1" || inner.calls[1].remoteCommand != "systemctl status app" {
		t.Fatalf("diagnostic call = %#v", inner.calls[1])
	}
	text := out.String()
	for _, want := range []string{
		"dbf debugger: running diagnostic command on server1",
		"----- BEGIN diagnostic command -----\nsystemctl status app\n----- END diagnostic command -----",
		"dbf debugger: diagnostic command succeeded",
		"----- BEGIN diagnostic stdout -----\nstatus output\n----- END diagnostic stdout -----",
		"----- BEGIN diagnostic stderr -----\njournal hint\n----- END diagnostic stderr -----",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostic output missing %q:\n%s", want, text)
		}
	}
}

func TestDebugRunnerQuitAllowsCleanupCalls(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("quit\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}

	_, err := runner.Run(context.Background(), "server1", "normal\n")
	if !errors.Is(err, ErrDebugCancelled) {
		t.Fatalf("normal run error = %v, want ErrDebugCancelled", err)
	}
	cleanupCtx := WithRemoteCallContext(context.Background(), RemoteCallContext{
		Phase:   "state unlock",
		Action:  "unlock",
		Cleanup: true,
	})
	if _, err := runner.Run(cleanupCtx, "server1", "unlock\n"); err != nil {
		t.Fatalf("cleanup run failed after quit: %v", err)
	}
	if len(inner.calls) != 1 || inner.calls[0].script != "unlock\n" {
		t.Fatalf("inner calls = %#v, want only cleanup", inner.calls)
	}
	text := out.String()
	if !strings.Contains(text, "phase: state unlock") || !strings.Contains(text, "cleanup: true") {
		t.Fatalf("cleanup output missing context:\n%s", text)
	}
}

func TestDebugRunnerCleanupCallDoesNotPrompt(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("quit\n"),
			Now:    steppedDebugClock(time.Unix(0, 0), time.Millisecond),
		}),
	}
	cleanupCtx := WithRemoteCallContext(context.Background(), RemoteCallContext{
		Phase:   "state unlock",
		Action:  "unlock",
		Cleanup: true,
	})

	if _, err := runner.Run(cleanupCtx, "server1", "unlock\n"); err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %#v, want cleanup call", inner.calls)
	}
	text := out.String()
	if strings.Contains(text, "(dbfdbg)") {
		t.Fatalf("cleanup call unexpectedly prompted:\n%s", text)
	}
}

func TestDebugRunnerMaintenanceCallBypassesInteractiveSession(t *testing.T) {
	inner := &debugRecordingRunner{}
	var out bytes.Buffer
	runner := DebugRunner{
		Inner: inner,
		Session: NewDebugSession(DebugSessionOptions{
			Writer: &out,
			Input:  strings.NewReader("quit\n"),
		}),
	}
	maintenanceCtx := WithRemoteCallContext(context.Background(), RemoteCallContext{
		Phase:       "state lock renew",
		Action:      "renew",
		Maintenance: true,
	})
	if _, err := runner.Run(maintenanceCtx, "server1", "renew\n"); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("maintenance call entered debugger output:\n%s", out.String())
	}
	if len(inner.calls) != 1 || inner.calls[0].script != "renew\n" {
		t.Fatalf("inner calls = %#v, want one maintenance call", inner.calls)
	}

	if _, err := runner.Run(context.Background(), "server1", "normal\n"); !errors.Is(err, ErrDebugCancelled) {
		t.Fatalf("normal call error = %v, want unread quit command to cancel", err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("normal call reached inner runner after quit: %#v", inner.calls)
	}
}

type debugRecordingRunner struct {
	calls    []debugRecordedCall
	contexts []RemoteCallContext
	result   Result
	err      error
}

type debugRecordedCall struct {
	runner        string
	host          string
	script        string
	remoteCommand string
	input         string
}

func (r *debugRecordingRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.recordContext(ctx)
	r.calls = append(r.calls, debugRecordedCall{runner: "Run", host: host, script: script})
	return r.result, r.err
}

func (r *debugRecordingRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	r.recordContext(ctx)
	data, err := io.ReadAll(input)
	if err != nil {
		return Result{}, err
	}
	r.calls = append(r.calls, debugRecordedCall{runner: "RunInput", host: host, remoteCommand: remoteCommand, input: string(data)})
	return r.result, r.err
}

func (r *debugRecordingRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	r.recordContext(ctx)
	r.calls = append(r.calls, debugRecordedCall{runner: "RunCommand", host: host, remoteCommand: remoteCommand})
	return r.result, r.err
}

func (r *debugRecordingRunner) recordContext(ctx context.Context) {
	if call, ok := RemoteCallContextFromContext(ctx); ok {
		r.contexts = append(r.contexts, call)
		return
	}
	r.contexts = append(r.contexts, RemoteCallContext{})
}

type debugSequenceRunner struct {
	calls   []debugRecordedCall
	results []Result
	errs    []error
}

func (r *debugSequenceRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.calls = append(r.calls, debugRecordedCall{runner: "Run", host: host, script: script})
	return r.next()
}

func (r *debugSequenceRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return Result{}, err
	}
	r.calls = append(r.calls, debugRecordedCall{runner: "RunInput", host: host, remoteCommand: remoteCommand, input: string(data)})
	return r.next()
}

func (r *debugSequenceRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	r.calls = append(r.calls, debugRecordedCall{runner: "RunCommand", host: host, remoteCommand: remoteCommand})
	return r.next()
}

func (r *debugSequenceRunner) next() (Result, error) {
	index := len(r.calls) - 1
	var result Result
	if index < len(r.results) {
		result = r.results[index]
	}
	var err error
	if index < len(r.errs) {
		err = r.errs[index]
	}
	return result, err
}

type contextRecordingProvider struct {
	*MemoryProvider
	applyContexts     []RemoteCallContext
	operationContexts []RemoteCallContext
}

func (p *contextRecordingProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	if call, ok := RemoteCallContextFromContext(ctx); ok {
		p.applyContexts = append(p.applyContexts, call)
	}
	return p.MemoryProvider.Apply(ctx, step)
}

func (p *contextRecordingProvider) RunOperation(ctx context.Context, operation graph.Operation) (OperationResult, error) {
	if call, ok := RemoteCallContextFromContext(ctx); ok {
		p.operationContexts = append(p.operationContexts, call)
	}
	return p.MemoryProvider.RunOperation(ctx, operation)
}

func steppedDebugClock(start time.Time, step time.Duration) func() time.Time {
	current := start.Add(-step)
	return func() time.Time {
		current = current.Add(step)
		return current
	}
}
