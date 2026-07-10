package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/merge"
	"github.com/mofelee/debianform/internal/core/parser"
	coreplan "github.com/mofelee/debianform/internal/core/plan"
	corestate "github.com/mofelee/debianform/internal/core/state"
	"github.com/mofelee/debianform/internal/core/termstyle"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestCompareActionMatrix(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.packages.install[\"curl\"]",
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{"name": "curl", "ensure": "present"},
	}
	digest := corestate.DesiredDigest(node.Desired)
	prior := &corestate.Resource{DesiredDigest: digest, Ownership: "managed"}

	tests := []struct {
		name     string
		prior    *corestate.Resource
		observed Observed
		want     string
	}{
		{name: "missing remote creates", observed: Observed{Exists: false}, want: ActionCreate},
		{name: "matching managed is no-op", prior: prior, observed: Observed{Exists: true, DesiredDigest: digest}, want: ActionNoOp},
		{name: "matching unmanaged adopts", observed: Observed{Exists: true, DesiredDigest: digest}, want: ActionAdopt},
		{name: "different remote updates", prior: prior, observed: Observed{Exists: true, DesiredDigest: "old"}, want: ActionUpdate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compare(node, tt.prior, tt.observed)
			if got.Action != tt.want {
				t.Fatalf("action = %q, want %q", got.Action, tt.want)
			}
		})
	}

	absent := node
	absent.Desired = map[string]any{"name": "curl", "ensure": "absent"}
	if got := Compare(absent, prior, Observed{Exists: true, DesiredDigest: digest}); got.Action != ActionDelete {
		t.Fatalf("absent desired action = %q, want delete", got.Action)
	}
}

func TestApplyWithMemoryProviderIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/foundation.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Create == 0 {
		t.Fatalf("initial apply summary = %#v, want creates", plan.Summary)
	}
	if len(provider.Applied) == 0 {
		t.Fatalf("provider did not apply any resources")
	}
	if len(provider.Operations) != 1 || provider.Operations[0] != "host.foundation1.systemd.daemon_reload" {
		t.Fatalf("operations = %#v, want daemon_reload", provider.Operations)
	}

	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 || next.Summary.Create != 0 || next.Summary.Update != 0 || next.Summary.Delete != 0 {
		t.Fatalf("second plan should be no-op, got steps=%#v summary=%#v", next.Steps, next.Summary)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "foundation apply state", string(data))
	for address, resource := range st.Resources {
		if resource.ProviderAddress == "" {
			t.Fatalf("state resource %s has no provider address", address)
		}
	}
	secretAddress := `host.foundation1.secrets.file["/etc/myapp/token"]`
	secret, ok := st.Resources[secretAddress]
	if !ok {
		t.Fatalf("state missing compatibility secret address %s", secretAddress)
	}
	if secret.Kind != "secret" || secret.ProviderType != "file" {
		t.Fatalf("state secret kind/provider = %s/%s, want secret/file", secret.Kind, secret.ProviderType)
	}
}

func TestApplyCancelsWorkAndReturnsLeaseLossCause(t *testing.T) {
	leaseCause := errors.New("injected lease renewal failure")
	lease := &testRenewableLock{lost: make(chan struct{})}
	backend := &testRenewableBackend{MemoryBackend: NewMemoryBackend(), lock: lease}
	provider := &leaseBlockingProvider{
		MemoryProvider: NewMemoryProvider(),
		started:        make(chan struct{}),
	}
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/lease-loss", nil)}}

	resultCh := make(chan error, 1)
	go func() {
		_, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
		resultCh <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider apply did not start")
	}
	lease.fail(leaseCause)

	select {
	case err := <-resultCh:
		if !errors.Is(err, leaseCause) {
			t.Fatalf("apply error = %v, want lease renewal root cause", err)
		}
		if count := strings.Count(err.Error(), leaseCause.Error()); count != 1 {
			t.Fatalf("lease renewal root cause appears %d times in error: %v", count, err)
		}
	case <-time.After(time.Second):
		t.Fatal("apply did not stop after lease loss")
	}
	lease.mu.Lock()
	unlocked := lease.unlocked
	lease.mu.Unlock()
	if !unlocked {
		t.Fatal("lease was not unlocked after renewal failure")
	}
}

func TestApplyReturnsUnlockErrorAfterSuccessfulExecution(t *testing.T) {
	unlockErr := errors.New("injected unlock failure")
	lock := &cleanupTestLock{err: unlockErr}
	backend := &cleanupTestBackend{
		MemoryBackend: NewMemoryBackend(),
		locks:         map[string]*cleanupTestLock{"server1": lock},
	}
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/unlock-error", nil)}}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if !errors.Is(err, unlockErr) {
		t.Fatalf("apply error = %v, want unlock failure", err)
	}
	if len(provider.Applied) != 1 {
		t.Fatalf("provider applied = %v, want successful main execution", provider.Applied)
	}
	if calls, _, _ := lock.snapshot(); calls != 1 {
		t.Fatalf("unlock calls = %d, want 1", calls)
	}
	if !strings.Contains(err.Error(), `unlock state for host "server1"`) {
		t.Fatalf("apply error lacks host context: %v", err)
	}
}

func TestApplyReturnsSSHBackendTokenMismatchAfterSuccessfulExecution(t *testing.T) {
	host := testBackendHost(t)
	backend := SSHBackend{
		Runner:        localShellRunner{},
		Owner:         "test",
		Warnings:      io.Discard,
		LeaseTTL:      3 * time.Hour,
		RenewInterval: time.Hour,
		RenewTimeout:  time.Second,
	}
	foreignToken := strings.Repeat("b", 32)
	provider := &afterApplyProvider{MemoryProvider: NewMemoryProvider()}
	provider.hook = func() error {
		data, err := os.ReadFile(host.State.LockPath)
		if err != nil {
			return err
		}
		var record testLockRecordData
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		if err := os.WriteFile(host.State.LockPath, testLockRecord("other", "999", foreignToken, record.LeaseExpiresAtUnix), 0600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(host.State.LockPath+".d", "owner.v2"), testLockMarker(foreignToken), 0600)
	}
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{host}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode(host.Name, "/tmp/ssh-token-mismatch", nil)}}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err == nil || !strings.Contains(err.Error(), "state lock token mismatch") {
		t.Fatalf("apply error = %v, want SSH token mismatch", err)
	}
	if len(provider.Applied) != 1 {
		t.Fatalf("provider applied = %v, want successful main execution", provider.Applied)
	}
	if _, statErr := os.Stat(host.State.LockPath); statErr != nil {
		t.Fatalf("foreign lease was removed after token mismatch: %v", statErr)
	}
}

func TestApplyUnlockUsesCleanupContextAfterCallerCancellation(t *testing.T) {
	cleanupErr := errors.New("cleanup after cancellation failed")
	lock := &cleanupTestLock{err: cleanupErr}
	backend := &cleanupTestBackend{
		MemoryBackend: NewMemoryBackend(),
		locks:         map[string]*cleanupTestLock{"server1": lock},
	}
	provider := &leaseBlockingProvider{
		MemoryProvider: NewMemoryProvider(),
		started:        make(chan struct{}),
	}
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/cancel-cleanup", nil)}}
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := engine.Apply(ctx, program, resourceGraph, Options{})
		resultCh <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider apply did not start")
	}
	cancel()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
			t.Fatalf("apply error = %v, want context canceled and cleanup failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("apply did not return after caller cancellation")
	}
	calls, sawCanceled, sawDeadline := lock.snapshot()
	if calls != 1 || sawCanceled || !sawDeadline {
		t.Fatalf("unlock context: calls=%d canceled=%t deadline=%t, want 1/false/true", calls, sawCanceled, sawDeadline)
	}
}

func TestApplyUnlockUsesCleanupContextAfterCallerDeadline(t *testing.T) {
	cleanupErr := errors.New("cleanup after deadline failed")
	lock := &cleanupTestLock{err: cleanupErr}
	backend := &cleanupTestBackend{
		MemoryBackend: NewMemoryBackend(),
		locks:         map[string]*cleanupTestLock{"server1": lock},
	}
	provider := &leaseBlockingProvider{
		MemoryProvider: NewMemoryProvider(),
		started:        make(chan struct{}),
	}
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/deadline-cleanup", nil)}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := engine.Apply(ctx, program, resourceGraph, Options{})
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, cleanupErr) {
		t.Fatalf("apply error = %v, want context deadline exceeded and cleanup failure", err)
	}
	calls, sawCanceled, sawDeadline := lock.snapshot()
	if calls != 1 || sawCanceled || !sawDeadline {
		t.Fatalf("unlock context: calls=%d canceled=%t deadline=%t, want 1/false/true", calls, sawCanceled, sawDeadline)
	}
}

func TestApplyJoinsAllHostUnlockErrors(t *testing.T) {
	hosts := []ir.HostSpec{{Name: "server1"}, {Name: "server2"}, {Name: "server3"}}
	unlockErrs := []error{
		errors.New("unlock server1 failed"),
		errors.New("unlock server2 failed"),
		errors.New("unlock server3 failed"),
	}
	locks := map[string]*cleanupTestLock{}
	for i, host := range hosts {
		locks[host.Name] = &cleanupTestLock{err: unlockErrs[i]}
	}
	backend := &cleanupTestBackend{MemoryBackend: NewMemoryBackend(), locks: locks}
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}

	_, err := engine.Apply(context.Background(), &ir.Program{Hosts: hosts}, &graph.ResourceGraph{}, Options{Parallel: 1})
	for i, unlockErr := range unlockErrs {
		if !errors.Is(err, unlockErr) {
			t.Fatalf("apply error = %v, want unlock error %v", err, unlockErr)
		}
		if calls, _, _ := locks[hosts[i].Name].snapshot(); calls != 1 {
			t.Fatalf("host %s unlock calls = %d, want 1", hosts[i].Name, calls)
		}
	}
}

func TestUnlockHostsUsesIndependentCleanupTimeouts(t *testing.T) {
	timedOut := &cleanupTestLock{waitForDeadline: true}
	afterErr := errors.New("later unlock failure")
	after := &cleanupTestLock{err: afterErr}
	locks := []heldLock{
		{host: ir.HostSpec{Name: "after"}, lock: after},
		{host: ir.HostSpec{Name: "timeout"}, lock: timedOut},
	}

	err := unlockHostsWithTimeout(context.Background(), locks, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, afterErr) {
		t.Fatalf("unlock error = %v, want timeout and later failure", err)
	}
	if calls, sawCanceled, sawDeadline := after.snapshot(); calls != 1 || sawCanceled || !sawDeadline {
		t.Fatalf("later cleanup context: calls=%d canceled=%t deadline=%t, want fresh context", calls, sawCanceled, sawDeadline)
	}
}

func TestLockHostsJoinsAcquireAndPartialCleanupErrors(t *testing.T) {
	acquireErr := errors.New("injected third host lock failure")
	cleanupErr1 := errors.New("server1 rollback failed")
	cleanupErr2 := errors.New("server2 rollback failed")
	var rollbackMu sync.Mutex
	rollbackOrder := []string{}
	lock1 := &cleanupTestLock{err: cleanupErr1, name: "server1", order: &rollbackOrder, orderMu: &rollbackMu}
	lock2 := &cleanupTestLock{err: cleanupErr2, name: "server2", order: &rollbackOrder, orderMu: &rollbackMu}
	backend := &cleanupTestBackend{
		MemoryBackend: NewMemoryBackend(),
		locks: map[string]*cleanupTestLock{
			"server1": lock1,
			"server2": lock2,
		},
		acquireErrs: map[string]error{"server3": acquireErr},
	}
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}, {Name: "server2"}, {Name: "server3"}}}

	_, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{Parallel: 1})
	for _, want := range []error{acquireErr, cleanupErr1, cleanupErr2} {
		if !errors.Is(err, want) {
			t.Fatalf("apply error = %v, want joined error %v", err, want)
		}
	}
	if calls, _, _ := lock1.snapshot(); calls != 1 {
		t.Fatalf("server1 rollback calls = %d, want 1", calls)
	}
	if calls, _, _ := lock2.snapshot(); calls != 1 {
		t.Fatalf("server2 rollback calls = %d, want 1", calls)
	}
	rollbackMu.Lock()
	gotOrder := append([]string(nil), rollbackOrder...)
	rollbackMu.Unlock()
	if want := []string{"server2", "server1"}; !reflect.DeepEqual(gotOrder, want) {
		t.Fatalf("rollback order = %v, want reverse acquisition order %v", gotOrder, want)
	}
}

func TestLockHostsRollsBackLockAcquiredAfterPeerCancellation(t *testing.T) {
	acquireErr := errors.New("injected peer lock failure")
	rollbackErr := errors.New("post-cancel rollback failure")
	slowLock := &cleanupTestLock{err: rollbackErr}
	backend := &postCancelAcquireBackend{
		MemoryBackend: NewMemoryBackend(),
		slowStarted:   make(chan struct{}),
		slowLock:      slowLock,
		acquireErr:    acquireErr,
	}
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "failing"}, {Name: "slow"}}}

	_, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{Parallel: 2})
	if !errors.Is(err, acquireErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("apply error = %v, want acquire and post-cancel rollback failures", err)
	}
	if calls, sawCanceled, sawDeadline := slowLock.snapshot(); calls != 1 || sawCanceled || !sawDeadline {
		t.Fatalf("post-cancel rollback: calls=%d canceled=%t deadline=%t, want 1/false/true", calls, sawCanceled, sawDeadline)
	}
}

func TestSystemHostnameMemoryPlanApplyCheckAndForget(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "web1" {
  system {
    hostname = "web-01"
  }
}
`))
	address := "host.web1.system.hostname"
	provider := NewMemoryProvider()
	provider.Observed[address] = Observed{
		Exists:        true,
		DesiredDigest: "drifted",
		Values:        map[string]any{"hostname": "old-host"},
	}
	backend := NewMemoryBackend()
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, address, ActionUpdate) {
		t.Fatalf("hostname drift plan missing update step: %#v", plan.Steps)
	}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	resource, ok := st.Resources[address]
	if !ok {
		t.Fatalf("state missing system hostname resource: %#v", st.Resources)
	}
	if resource.Kind != "system_hostname" || resource.ProviderType != "system_hostname" || resource.Observed["hostname"] != "web-01" {
		t.Fatalf("hostname state resource = %#v", resource)
	}

	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 {
		t.Fatalf("matching hostname check should be no-op, got steps=%#v", next.Steps)
	}

	emptyProgram, emptyGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `host "web1" {}`))
	forgetPlan, err := engine.Plan(context.Background(), emptyProgram, emptyGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(forgetPlan, address, ActionForget) {
		t.Fatalf("omitted hostname should forget state, got steps=%#v", forgetPlan.Steps)
	}
	if _, err := engine.Apply(context.Background(), emptyProgram, emptyGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("omitted hostname should not destroy remote resource: %#v", provider.Destroyed)
	}
	st, err = backend.Read(context.Background(), emptyProgram.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[address]; ok {
		t.Fatalf("system hostname state was not forgotten: %#v", st.Resources)
	}
}

func TestSystemTimezoneLocaleMemoryPlanApplyCheckAndForget(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "web1" {
  system {
    timezone = "Asia/Shanghai"
    locale   = "en_US.UTF-8"
  }
}
`))
	timezoneAddress := "host.web1.system.timezone"
	localeAddress := "host.web1.system.locale"
	provider := NewMemoryProvider()
	provider.Observed[timezoneAddress] = Observed{
		Exists:        true,
		DesiredDigest: "drifted",
		Values:        map[string]any{"timezone": "UTC", "zone_exists": true},
	}
	provider.Observed[localeAddress] = Observed{
		Exists:        true,
		DesiredDigest: "drifted",
		Values:        map[string]any{"locale": "C", "available": true},
	}
	backend := NewMemoryBackend()
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{timezoneAddress, localeAddress} {
		if !hasStepAction(plan, address, ActionUpdate) {
			t.Fatalf("%s drift plan missing update step: %#v", address, plan.Steps)
		}
	}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	timezone := st.Resources[timezoneAddress]
	if timezone.Kind != "system_timezone" || timezone.ProviderType != "system_timezone" || timezone.Observed["timezone"] != "Asia/Shanghai" {
		t.Fatalf("timezone state resource = %#v", timezone)
	}
	locale := st.Resources[localeAddress]
	if locale.Kind != "system_locale" || locale.ProviderType != "system_locale" || locale.Observed["locale"] != "en_US.UTF-8" {
		t.Fatalf("locale state resource = %#v", locale)
	}

	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 {
		t.Fatalf("matching timezone/locale check should be no-op, got steps=%#v", next.Steps)
	}

	emptyProgram, emptyGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `host "web1" {}`))
	forgetPlan, err := engine.Plan(context.Background(), emptyProgram, emptyGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{timezoneAddress, localeAddress} {
		if !hasStepAction(forgetPlan, address, ActionForget) {
			t.Fatalf("omitted %s should forget state, got steps=%#v", address, forgetPlan.Steps)
		}
	}
	if _, err := engine.Apply(context.Background(), emptyProgram, emptyGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("omitted timezone/locale should not destroy remote resources: %#v", provider.Destroyed)
	}
	st, err = backend.Read(context.Background(), emptyProgram.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{timezoneAddress, localeAddress} {
		if _, ok := st.Resources[address]; ok {
			t.Fatalf("%s state was not forgotten: %#v", address, st.Resources)
		}
	}
}

func TestApplyWritesProgress(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	var progress bytes.Buffer
	engine := Engine{Backend: NewMemoryBackend(), Provider: NewMemoryProvider()}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Progress: &progress}); err != nil {
		t.Fatal(err)
	}

	output := progress.String()
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("progress output contains ANSI:\n%q", output)
	}
	for _, want := range []string{
		"dbf: server1: start lock state",
		`dbf: server1: start create host.server1.files.file["/tmp/a"] - create file /tmp/a`,
		`dbf: server1: done create host.server1.files.file["/tmp/a"] - create file /tmp/a`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("progress output missing %q:\n%s", want, output)
		}
	}
}

func TestProgressTaskLogsHeartbeat(t *testing.T) {
	var output bytes.Buffer
	progress := newProgressLogger(&output)
	progress.interval = 10 * time.Millisecond

	task := progress.Start("server1", "apply", `host.server1.files.file["/tmp/slow"]`, "write file /tmp/slow")
	time.Sleep(25 * time.Millisecond)
	task.Done(nil)

	text := output.String()
	if !strings.Contains(text, `dbf: server1: still apply host.server1.files.file["/tmp/slow"] - write file /tmp/slow`) {
		t.Fatalf("progress output missing heartbeat:\n%s", text)
	}
}

func TestProgressTaskCanUseStyledStatusBadges(t *testing.T) {
	var output bytes.Buffer
	progress := newProgressLoggerWithStyle(&output, termstyle.Options{Color: true, Unicode: true, Background: true})
	progress.interval = time.Hour

	task := progress.Start("server1", "create", `host.server1.files.file["/tmp/a"]`, "create file /tmp/a")
	task.Done(nil)

	text := output.String()
	for _, want := range []string{
		"\x1b[1m\x1b[97m\x1b[44m ▶ START \x1b[0m",
		"\x1b[1m\x1b[30m\x1b[42m ✓ DONE \x1b[0m",
		"\x1b[1m\x1b[36mserver1:\x1b[0m",
		"\x1b[32mcreate\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("styled progress output missing %q:\n%q", want, text)
		}
	}
}

func TestApplyStateDoesNotLeakCurrentSensitiveBaseline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
		host    string
	}{
		{name: "secrets file", fixture: "../testdata/fixtures/foundation.dbf.hcl", host: "foundation1"},
		{name: "sensitive file content", fixture: "../../../examples/files-plan-preview.dbf.hcl", host: "preview1"},
		{name: "sensitive component input", fixture: "../../../examples/component-inputs.dbf.hcl", host: "input1"},
		{name: "sensitive service environment", fixture: "../testdata/fixtures/sensitive-service-environment.dbf.hcl", host: "server1"},
		{name: "sensitive apt and nftables content", fixture: "../testdata/fixtures/sensitive-apt-nftables-content.dbf.hcl", host: "server1"},
		{name: "ephemeral variable content", fixture: "../testdata/fixtures/ephemeral-variable-content.dbf.hcl", host: "ephemeral1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			program, resourceGraph := fixtureProgramAndGraph(t, tt.fixture)
			backend := NewMemoryBackend()
			engine := Engine{
				Backend:  backend,
				Provider: NewMemoryProvider(),
				Now: func() time.Time {
					return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
				},
			}
			if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: tt.host}); err != nil {
				t.Fatal(err)
			}
			var host ir.HostSpec
			for _, candidate := range program.Hosts {
				if candidate.Name == tt.host {
					host = candidate
					break
				}
			}
			if host.Name == "" {
				t.Fatalf("host %q not found", tt.host)
			}
			st, err := backend.Read(context.Background(), host)
			if err != nil {
				t.Fatal(err)
			}
			data, err := corestate.Encode(st)
			if err != nil {
				t.Fatal(err)
			}
			testassert.NoSecretLeak(t, tt.name+" apply state", string(data))
		})
	}
}

func TestApplySensitiveAPTAndNftablesStateStoresOnlySummaries(t *testing.T) {
	fixture := "../testdata/fixtures/sensitive-apt-nftables-content.dbf.hcl"
	program, resourceGraph := fixtureProgramAndGraph(t, fixture)
	backend := NewMemoryBackend()
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "server1"}); err != nil {
		t.Fatal(err)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{
		`host.server1.apt.source_file["private"]`,
		`host.server1.apt.signing_key["private"]`,
		`host.server1.components.private_apt.apt.source_file["component-private"]`,
		`host.server1.components.private_apt.apt.signing_key["component-private"]`,
		`host.server1.nftables.file["main"]`,
		`host.server1.nftables.file["private"]`,
	} {
		resource, ok := st.Resources[address]
		if !ok {
			t.Fatalf("state missing sensitive resource %s", address)
		}
		if resource.Desired["sensitive"] != true || resource.Desired["content_sha256"] == "" {
			t.Fatalf("state resource %s missing sensitive summary: %#v", address, resource.Desired)
		}
		for _, key := range []string{"content", "source_path", "summary"} {
			if _, ok := resource.Desired[key]; ok {
				t.Fatalf("state resource %s contains %s: %#v", address, key, resource.Desired)
			}
		}
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "sensitive apt and nftables structured state", string(data))
}

func TestApplyWriteOnlyFilePersistsVersionAndPassesPayload(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/ephemeral-variable-content.dbf.hcl")
	backend := NewMemoryBackend()
	provider := &recordingPayloadProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "ephemeral1"}); err != nil {
		t.Fatal(err)
	}
	address := `host.ephemeral1.files.file["/etc/debianform/runtime-token.txt"]`
	payload := provider.Payloads[address]
	if payload["content"] != testassert.EphemeralVariableValue {
		t.Fatalf("provider payload content = %#v, want ephemeral value", payload["content"])
	}
	host := program.Hosts[0]
	st, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	resource := st.Resources[address]
	if resource.Desired["content_version"] != "v1" {
		t.Fatalf("state content_version = %#v, want v1", resource.Desired["content_version"])
	}
	for _, key := range []string{"content", "content_sha256", "content_bytes", "summary"} {
		if _, ok := resource.Desired[key]; ok {
			t.Fatalf("state desired contains %s: %#v", key, resource.Desired)
		}
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "write-only apply state", string(data))
}

func TestApplyPersistsOnlySuccessfulStepsOnFailure(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	provider.FailApplyAt = `host.bbr1.kernel.sysctl["net.core.default_qdisc"]`
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err == nil {
		t.Fatal("apply succeeded, want injected failure")
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[`host.bbr1.kernel.module["tcp_bbr"]`]; !ok {
		t.Fatalf("successful dependency was not persisted: %#v", st.Resources)
	}
	if _, ok := st.Resources[`host.bbr1.kernel.sysctl["net.core.default_qdisc"]`]; ok {
		t.Fatalf("failed step was persisted: %#v", st.Resources)
	}
}

func TestApplyDestroysOrphanStateResource(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.packages.install["curl"]`
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "bbr1",
		Resources: map[string]corestate.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "package",
				Ownership:     "managed",
				DesiredDigest: "old",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Destroyed) != 1 || provider.Destroyed[0] != orphanAddress {
		t.Fatalf("destroyed = %#v, want %s", provider.Destroyed, orphanAddress)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("orphan still in state: %#v", st.Resources)
	}
}

func TestApplyForgetsSharedDirectoryOrphanWithoutDestroy(t *testing.T) {
	program := &ir.Program{
		Hosts: []ir.HostSpec{{
			Name: "server1",
		}},
	}
	activeAddress := `host.server1.components.wg_backup.directories.directory["/etc/wireguard"]`
	orphanAddress := `host.server1.components.wg_prod.directories.directory["/etc/wireguard"]`
	desired := map[string]any{
		"path":   "/etc/wireguard",
		"owner":  "root",
		"group":  "systemd-network",
		"mode":   "0750",
		"ensure": "present",
	}
	resourceGraph := &graph.ResourceGraph{
		Nodes: []graph.Node{{
			Address:         activeAddress,
			Host:            "server1",
			Kind:            "directory",
			Summary:         "create directory /etc/wireguard",
			Desired:         cloneMap(desired),
			ProviderType:    "directory",
			ProviderAddress: "directory.server1_wg_backup_etc_wireguard",
			ProviderPayload: cloneMap(desired),
		}},
	}
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "server1",
		Resources: map[string]corestate.Resource{
			activeAddress: {
				Host:            "server1",
				Kind:            "directory",
				ProviderType:    "directory",
				ProviderAddress: "directory.server1_wg_backup_etc_wireguard",
				Ownership:       "managed",
				Desired:         cloneMap(desired),
				DesiredDigest:   corestate.DesiredDigest(desired),
				Observed:        map[string]any{"exists": true},
			},
			orphanAddress: {
				Host:            "server1",
				Kind:            "directory",
				ProviderType:    "directory",
				ProviderAddress: "directory.server1_wg_prod_etc_wireguard",
				Ownership:       "managed",
				Desired:         cloneMap(desired),
				DesiredDigest:   corestate.DesiredDigest(desired),
				Observed:        map[string]any{"exists": true},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget shared directory orphan", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("shared directory orphan still in state: %#v", st.Resources)
	}
	if _, ok := st.Resources[activeAddress]; !ok {
		t.Fatalf("active shared directory missing from state: %#v", st.Resources)
	}
}

func TestApplyForgetsAdoptedOrphanWithoutDestroy(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.packages.install["curl"]`
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "bbr1",
		Resources: map[string]corestate.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "package",
				Ownership:     "adopted",
				DesiredDigest: "old",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("adopted orphan still in state: %#v", st.Resources)
	}
}

func TestApplyForgetsAPTSourceFileOrphanWhenDestroyKeeps(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.apt.source_file["main"]`
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "bbr1",
		Resources: map[string]corestate.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "apt_source_file",
				Ownership:     "managed",
				DesiredDigest: "old",
				Desired:       map[string]any{"path": "/etc/apt/sources.list", "on_destroy": "keep"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
}

func TestBBRMemoryApplyIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	engine := Engine{Backend: NewMemoryBackend(), Provider: NewMemoryProvider()}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 {
		t.Fatalf("second BBR plan steps = %#v, want no-op", next.Steps)
	}
}

func TestDockerEngineMemoryApplyIsIdempotentAndPersistsHighLevelState(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-minimal.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Create != 8 || plan.Summary.Operations != 1 {
		t.Fatalf("docker apply summary = %#v, want 8 creates and 1 operation", plan.Summary)
	}
	if len(provider.Operations) != 1 || provider.Operations[0] != "host.docker1.apt.cache_refresh" {
		t.Fatalf("docker operations = %#v, want apt cache refresh", provider.Operations)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		address         string
		kind            string
		providerType    string
		providerAddress string
	}{
		{
			address:         `host.docker1.docker.apt.signing_key["docker-official"]`,
			kind:            "apt_signing_key",
			providerType:    "apt_signing_key",
			providerAddress: "apt_signing_key.docker1_docker_official",
		},
		{
			address:         `host.docker1.docker.apt.repository["docker-official"]`,
			kind:            "file",
			providerType:    "file",
			providerAddress: "file.docker1__etc_apt_sources_list_d_docker_official_sources",
		},
		{
			address:         `host.docker1.docker.package["docker-ce"]`,
			kind:            "package",
			providerType:    "package",
			providerAddress: "package.docker1_docker_docker_ce",
		},
		{
			address:         `host.docker1.docker.service["docker"]`,
			kind:            "service",
			providerType:    "service",
			providerAddress: "service.docker1_docker",
		},
	} {
		resource, ok := st.Resources[want.address]
		if !ok {
			t.Fatalf("docker state missing %s; resources=%#v", want.address, st.Resources)
		}
		if resource.Kind != want.kind || resource.ProviderType != want.providerType || resource.ProviderAddress != want.providerAddress {
			t.Fatalf("state resource %s kind/provider = %s/%s/%s, want %s/%s/%s", want.address, resource.Kind, resource.ProviderType, resource.ProviderAddress, want.kind, want.providerType, want.providerAddress)
		}
		if resource.Ownership != "managed" {
			t.Fatalf("state resource %s ownership = %q, want managed", want.address, resource.Ownership)
		}
	}

	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 || next.Summary.Create != 0 || next.Summary.Update != 0 || next.Summary.Delete != 0 || next.Summary.Operations != 0 {
		t.Fatalf("second docker plan should be no-op, got steps=%#v summary=%#v", next.Steps, next.Summary)
	}
}

func TestDockerDaemonDesiredChangePlansRestartOperation(t *testing.T) {
	initial := `
host "server1" {
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "100m"
          "max-file" = "3"
        }
      }
    }
  }
}
`
	updated := strings.Replace(initial, `"100m"`, `"200m"`, 1)
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, initial))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}

	nextProgram, nextGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, updated))
	plan, err := engine.Plan(context.Background(), nextProgram, nextGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.server1.docker.daemon.file["/etc/docker/daemon.json"]`, ActionUpdate) {
		t.Fatalf("daemon desired change plan missing daemon file update: %#v", plan.Steps)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Operation.Address != "host.server1.docker.daemon.restart" {
		t.Fatalf("daemon desired change operations = %#v, want docker daemon restart", plan.Operations)
	}
}

func TestDockerComposeMemoryApplyDoesNotLeakEnvFileContent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.compose1.docker.compose["app"].env_file["app"]`, ActionCreate) {
		t.Fatalf("compose apply plan missing env file create: %#v", plan.Steps)
	}
	if len(provider.Operations) != 3 ||
		!containsString(provider.Operations, `host.compose1.docker.compose["app"].validate`) ||
		!containsString(provider.Operations, `host.compose1.docker.compose["app"].daemon_reload`) {
		t.Fatalf("compose operations = %#v, want apt refresh, compose daemon_reload, and compose validate", provider.Operations)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "compose apply state", string(data))
	env, ok := st.Resources[`host.compose1.docker.compose["app"].env_file["app"]`]
	if !ok {
		t.Fatalf("state missing compose env file: %#v", st.Resources)
	}
	if _, ok := env.Desired["content"]; ok {
		t.Fatalf("state env desired leaked content: %#v", env.Desired)
	}
	if env.Desired["content_sha256"] == "" || env.Desired["content_bytes"] == nil {
		t.Fatalf("state env desired missing content summary: %#v", env.Desired)
	}
}

func TestDockerComposeMemoryApplyIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "compose1"}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 || next.Summary.Create != 0 || next.Summary.Update != 0 || next.Summary.Delete != 0 || next.Summary.Operations != 0 {
		t.Fatalf("second compose plan should be no-op, got steps=%#v summary=%#v", next.Steps, next.Summary)
	}
}

func TestDockerComposeMemoryApplyIncludesSystemdUnitAndService(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	backend := NewMemoryBackend()
	provider := &recordingPayloadProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "compose1"}); err != nil {
		t.Fatal(err)
	}
	if !containsString(provider.Operations, `host.compose1.docker.compose["app"].daemon_reload`) {
		t.Fatalf("compose operations = %#v, want daemon_reload", provider.Operations)
	}
	unitPayload := provider.Payloads[`host.compose1.docker.compose["app"].systemd_unit`]
	content, _ := unitPayload["content"].(string)
	if !strings.Contains(content, "ExecStart=/usr/bin/docker compose -p app -f /opt/app/compose.yaml up -d") {
		t.Fatalf("compose unit payload missing ExecStart:\n%s", content)
	}
	servicePayload := provider.Payloads[`host.compose1.docker.compose["app"].service`]
	if servicePayload["unit"] != "debianform-compose-app.service" || servicePayload["enabled"] != true || servicePayload["state"] != "running" {
		t.Fatalf("compose service payload = %#v", servicePayload)
	}
}

func TestComponentScriptOnChangeOperationRunsAfterFileChange(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/component-script-on-change.dbf.hcl")
	provider := NewMemoryProvider()
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	fileAddress := `host.app1.components.app.files.file["/etc/managed-app/config.env"]`
	scriptAddress := `host.app1.components.app.script["reload"]`
	if !hasStepAction(plan, fileAddress, ActionCreate) {
		t.Fatalf("apply plan missing component file create: %#v", plan.Steps)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Operation.Address != scriptAddress {
		t.Fatalf("apply operations = %#v, want script reload", plan.Operations)
	}
	if !containsString(provider.Operations, scriptAddress) {
		t.Fatalf("provider operations = %#v, want script reload", provider.Operations)
	}
}

func TestComponentScriptOutputsTriggerOperationAndRecordState(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/component-script-on-change.dbf.hcl")
	backend := NewMemoryBackend()
	provider := &recordingOperationProvider{MemoryProvider: NewMemoryProvider(), OutputSHA: "rendered-sha"}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	outputAddress := `host.app1.components.app.script["reload"].outputs["/etc/managed-app/rendered.env"]`
	scriptAddress := `host.app1.components.app.script["reload"]`
	if !hasStepAction(plan, outputAddress, ActionCreate) {
		t.Fatalf("apply plan missing script output refresh: %#v", plan.Steps)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Operation.Address != scriptAddress {
		t.Fatalf("apply operations = %#v, want script reload", plan.Operations)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	output, ok := st.Resources[outputAddress]
	if !ok {
		t.Fatalf("script output state missing from %#v", st.Resources)
	}
	if output.Kind != "component_script_output" || output.Observed["sha256"] != "rendered-sha" {
		t.Fatalf("script output state = %#v", output)
	}
	payloads := provider.ScriptPayloads[scriptAddress]
	if len(payloads) != 1 || len(payloads[0].Outputs) != 1 || payloads[0].Outputs[0].Path != "/etc/managed-app/rendered.env" {
		t.Fatalf("script payload outputs = %#v", payloads)
	}
}

func TestComponentScriptOutputDriftRerunsOperation(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/component-script-on-change.dbf.hcl")
	backend := NewMemoryBackend()
	outputAddress := `host.app1.components.app.script["reload"].outputs["/etc/managed-app/rendered.env"]`
	scriptAddress := `host.app1.components.app.script["reload"]`
	backend.states["app1"] = corestate.State{
		Version: corestate.Version,
		Host:    "app1",
		Resources: map[string]corestate.Resource{
			outputAddress: {
				Host:          "app1",
				Kind:          "component_script_output",
				Ownership:     "managed",
				Desired:       map[string]any{"path": "/etc/managed-app/rendered.env", "component": "app", "script": "reload"},
				DesiredDigest: corestate.DesiredDigest(map[string]any{"path": "/etc/managed-app/rendered.env", "component": "app", "script": "reload"}),
				Observed:      map[string]any{"exists": true, "is_dir": false, "sha256": "old-sha", "path": "/etc/managed-app/rendered.env"},
			},
		},
	}
	provider := &recordingOperationProvider{MemoryProvider: NewMemoryProvider(), OutputSHA: "new-sha"}
	provider.Observed[outputAddress] = Observed{
		Exists:        true,
		DesiredDigest: "old-sha",
		Values:        map[string]any{"exists": true, "is_dir": false, "sha256": "drifted-sha", "path": "/etc/managed-app/rendered.env"},
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, outputAddress, ActionUpdate) {
		t.Fatalf("drift plan missing script output update: %#v", plan.Steps)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Operation.Address != scriptAddress {
		t.Fatalf("drift operations = %#v, want script rerun", plan.Operations)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Resources[outputAddress].Observed["sha256"]; got != "new-sha" {
		t.Fatalf("script output sha after rerun = %#v, want new-sha", got)
	}
}

func TestComponentScriptOnceRunsOnceForMultipleChangedFiles(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, componentScriptModeFixture("once")))
	provider := &recordingOperationProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	scriptAddress := `host.app1.components.app.script["reload"]`
	if len(plan.Operations) != 1 || plan.Operations[0].Address != scriptAddress {
		t.Fatalf("once operations = %#v, want one script operation", plan.Operations)
	}
	payloads := provider.ScriptPayloads[scriptAddress]
	if len(payloads) != 1 {
		t.Fatalf("once script payloads = %#v, want one run", payloads)
	}
	if strings.Join(payloads[0].TriggerPaths, "\n") != "/etc/app.conf\n/etc/app.extra" {
		t.Fatalf("once trigger paths = %#v, want both paths", payloads[0].TriggerPaths)
	}
}

func TestComponentScriptEachRunsForEveryChangedFile(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, componentScriptModeFixture("each")))
	provider := &recordingOperationProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	baseAddress := `host.app1.components.app.script["reload"]`
	if len(plan.Operations) != 2 {
		t.Fatalf("each operations = %#v, want two script operations", plan.Operations)
	}
	for _, op := range plan.Operations {
		if op.Address == baseAddress || !strings.HasPrefix(op.Address, baseAddress+`.trigger[`) {
			t.Fatalf("each operation address = %q, want unique trigger address", op.Address)
		}
	}
	payloads := provider.ScriptPayloads[baseAddress]
	if len(payloads) != 2 {
		t.Fatalf("each script payloads = %#v, want two runs", payloads)
	}
	gotPaths := []string{strings.Join(payloads[0].TriggerPaths, "\n"), strings.Join(payloads[1].TriggerPaths, "\n")}
	sort.Strings(gotPaths)
	wantPaths := []string{"/etc/app.conf", "/etc/app.extra"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("each trigger paths = %#v, want %#v", gotPaths, wantPaths)
	}
	for _, payload := range payloads {
		if len(payload.TriggerAddresses) != 1 || len(payload.TriggerPaths) != 1 {
			t.Fatalf("each payload should have one trigger: %#v", payload)
		}
	}
}

func TestComponentScriptEachPlanDocumentUsesStableTriggerAddresses(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, componentScriptModeFixture("each")))
	engine := Engine{Backend: NewMemoryBackend(), Provider: NewMemoryProvider()}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc := plan.Document(coreplan.Options{
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	if doc.Summary.Operations != 2 || len(doc.Operations) != 2 {
		t.Fatalf("plan document operations = %#v, want two", doc.Operations)
	}
	for _, op := range doc.Operations {
		if len(op.TriggeredBy) != 1 || len(op.DependsOn) != 1 || op.TriggeredBy[0] != op.DependsOn[0] {
			t.Fatalf("each operation deps/triggers = %#v/%#v, want one matching trigger", op.DependsOn, op.TriggeredBy)
		}
		if !strings.Contains(op.Address, ".trigger["+fmt.Sprintf("%q", op.TriggeredBy[0])+"]") {
			t.Fatalf("each operation address = %q, trigger = %q", op.Address, op.TriggeredBy[0])
		}
	}
	var text bytes.Buffer
	coreplan.PrintText(&text, doc)
	rendered := text.String()
	for _, want := range []string{
		`host.app1.components.app.script["reload"].trigger["host.app1.components.app.files.file[\"/etc/app.conf\"]"]`,
		`host.app1.components.app.script["reload"].trigger["host.app1.components.app.files.file[\"/etc/app.extra\"]"]`,
		`command: script reload (each)`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("plan text missing %q:\n%s", want, rendered)
		}
	}
}

func TestDockerComposeMemoryCheckDetectsStoppedProjectDrift(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	provider := NewMemoryProvider()
	provider.Observed[`host.compose1.docker.compose["app"].project`] = Observed{Exists: true, DesiredDigest: "drifted", Values: map[string]any{
		"exists": true,
		"state":  "stopped",
		"services": map[string]any{
			"total":   1,
			"running": 0,
			"stopped": 1,
		},
	}}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.compose1.docker.compose["app"].project`, ActionUpdate) {
		t.Fatalf("compose stopped drift plan missing project update: %#v", plan.Steps)
	}
}

func TestDockerComposeDriftPlanTextShowsProjectStateAndOrphans(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	provider := NewMemoryProvider()
	provider.Observed[`host.compose1.docker.compose["app"].project`] = Observed{Exists: true, DesiredDigest: "drifted", Values: map[string]any{
		"exists": true,
		"state":  "running",
		"services": map[string]any{
			"total":    2,
			"running":  2,
			"stopped":  0,
			"expected": []string{"web"},
			"actual":   []string{"web", "worker"},
		},
		"containers":      map[string]any{"total": 2},
		"orphan_count":    1,
		"orphan_services": []string{"worker"},
	}}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	doc := plan.Document(coreplan.Options{CommandFile: "../../../examples/docker-compose.dbf.hcl", Host: "compose1"})
	var text bytes.Buffer
	coreplan.PrintText(&text, doc)
	got := text.String()
	assertGolden(t, "../testdata/plan/docker-compose-project-drift.golden.txt", got)
	for _, want := range []string{
		"orphan_count: 1",
		`"worker"`,
		`services.actual`,
		`services.expected`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compose drift text missing %q:\n%s", want, got)
		}
	}
}

func TestDockerEngineMemoryCheckDetectsPackageAndServiceDrift(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-minimal.dbf.hcl")
	provider := NewMemoryProvider()
	provider.Observed[`host.docker1.docker.package["docker-ce"]`] = Observed{Exists: false}
	provider.Observed[`host.docker1.docker.service["docker"]`] = Observed{Exists: true, DesiredDigest: "drifted", Values: map[string]any{
		"enabled": false,
		"active":  false,
	}}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.docker1.docker.package["docker-ce"]`, ActionCreate) {
		t.Fatalf("docker drift plan missing package create step: %#v", plan.Steps)
	}
	if !hasStepAction(plan, `host.docker1.docker.service["docker"]`, ActionUpdate) {
		t.Fatalf("docker drift plan missing service update step: %#v", plan.Steps)
	}
	if len(plan.Steps) == 0 {
		t.Fatalf("docker drift plan has no steps; check would incorrectly pass")
	}
}

func TestPlanPropagatesObservedReadFailure(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	engine := Engine{
		Backend:  NewMemoryBackend(),
		Provider: planErrorProvider{MemoryProvider: NewMemoryProvider()},
	}

	_, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err == nil || !strings.Contains(err.Error(), "injected observed read failure") {
		t.Fatalf("plan error = %v, want observed read failure", err)
	}
}

func TestApplyRunsHostsInParallelWithDeterministicResultOrder(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server2" {
  files {
    file "/tmp/server2" {
      content = "server2"
    }
  }
}

host "server1" {
  files {
    file "/tmp/server1" {
      content = "server1"
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2})
	if err != nil {
		t.Fatal(err)
	}
	if provider.maxActive < 2 {
		t.Fatalf("max concurrent applies = %d, want at least 2", provider.maxActive)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v, want 2", plan.Steps)
	}
	if plan.Steps[0].Host != "server1" || plan.Steps[1].Host != "server2" {
		t.Fatalf("step host order = %s, %s; want server1, server2", plan.Steps[0].Host, plan.Steps[1].Host)
	}
	for _, host := range program.Hosts {
		st, err := backend.Read(context.Background(), host)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Resources) != 1 {
			t.Fatalf("host %s state resources = %#v, want 1", host.Name, st.Resources)
		}
	}
}

func TestPlanReadsStateAndInspectsHostsInParallel(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}, {Name: "server2"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/server1", nil),
		fileNode("server2", "/tmp/server2", nil),
	}}
	backend := &concurrencyBackend{Backend: NewMemoryBackend()}
	provider := &hostPlanningConcurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Plan(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	if backend.maxReadActive < 2 {
		t.Fatalf("max concurrent state reads = %d, want cross-host reads in parallel", backend.maxReadActive)
	}
	if provider.maxPlanHostActive < 2 {
		t.Fatalf("max concurrent host plans = %d, want cross-host inspect in parallel", provider.maxPlanHostActive)
	}
}

func TestPlanHonorsDefaultHostParallelLimit(t *testing.T) {
	program := &ir.Program{Hosts: make([]ir.HostSpec, 8)}
	resourceGraph := &graph.ResourceGraph{Nodes: make([]graph.Node, 0, 8)}
	for i := range program.Hosts {
		name := fmt.Sprintf("server%d", i+1)
		program.Hosts[i] = ir.HostSpec{Name: name}
		resourceGraph.Nodes = append(resourceGraph.Nodes, fileNode(name, "/tmp/"+name, nil))
	}
	backend := &concurrencyBackend{Backend: NewMemoryBackend()}
	provider := &hostPlanningConcurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Plan(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	if backend.maxReadActive != defaultHostParallel() {
		t.Fatalf("max concurrent state reads = %d, want default %d", backend.maxReadActive, defaultHostParallel())
	}
	if provider.maxPlanHostActive != defaultHostParallel() {
		t.Fatalf("max concurrent host plans = %d, want default %d", provider.maxPlanHostActive, defaultHostParallel())
	}
}

func TestPlanHonorsExplicitHostParallelLimit(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}, {Name: "server2"}, {Name: "server3"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/server1", nil),
		fileNode("server2", "/tmp/server2", nil),
		fileNode("server3", "/tmp/server3", nil),
	}}
	backend := &concurrencyBackend{Backend: NewMemoryBackend()}
	provider := &hostPlanningConcurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Plan(context.Background(), program, resourceGraph, Options{Parallel: 1}); err != nil {
		t.Fatal(err)
	}
	if backend.maxReadActive != 1 {
		t.Fatalf("max concurrent state reads = %d, want 1", backend.maxReadActive)
	}
	if provider.maxPlanHostActive != 1 {
		t.Fatalf("max concurrent host plans = %d, want 1", provider.maxPlanHostActive)
	}
}

func TestApplyHonorsDefaultPerHostSerialExecution(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive != 1 {
		t.Fatalf("max concurrent applies = %d, want default per-host serial execution", provider.maxActive)
	}
}

func TestApplyAllowsSafeParallelWithinHostWhenConfigured(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2, PerHostParallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive < 2 {
		t.Fatalf("max concurrent applies = %d, want safe same-host resources to run in parallel", provider.maxActive)
	}
}

func TestApplyGlobalParallelLimit(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}, {Name: "server2"}, {Name: "server3"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/server1", nil),
		fileNode("server2", "/tmp/server2", nil),
		fileNode("server3", "/tmp/server3", nil),
	}}
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive != 2 {
		t.Fatalf("max concurrent applies = %d, want global limit 2", provider.maxActive)
	}
}

func TestApplySkipsDependentsAfterFailure(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	failing := `host.server1.files.file["/tmp/failing"]`
	dependent := `host.server1.files.file["/tmp/dependent"]`
	independent := `host.server1.files.file["/tmp/independent"]`
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/failing", nil),
		fileNode("server1", "/tmp/independent", nil),
		fileNode("server1", "/tmp/dependent", []string{failing}),
	}}
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	provider.FailApplyAt = failing
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2, PerHostParallel: 2})
	if err == nil || !strings.Contains(err.Error(), failing) {
		t.Fatalf("apply error = %v, want failing resource", err)
	}
	if containsString(provider.Applied, dependent) {
		t.Fatalf("dependent resource was applied after failed dependency: %#v", provider.Applied)
	}
	if !containsString(provider.Applied, independent) {
		t.Fatalf("independent resource was not applied after unrelated failure: %#v", provider.Applied)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[independent]; !ok {
		t.Fatalf("independent resource was not persisted: %#v", st.Resources)
	}
	if _, ok := st.Resources[dependent]; ok {
		t.Fatalf("dependent resource was persisted despite skipped dependency: %#v", st.Resources)
	}
}

func TestPreventDestroyBlocksOrphanDestroy(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  files {
    file "/tmp/protected" {
      content = "managed"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}

	emptyProgram, emptyGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {}
`))
	_, err := engine.Apply(context.Background(), emptyProgram, emptyGraph, Options{})
	if err == nil {
		t.Fatal("apply succeeded, want prevent_destroy error")
	}
	if !strings.Contains(err.Error(), "lifecycle.prevent_destroy") {
		t.Fatalf("error = %v, want prevent_destroy", err)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no destroy", provider.Destroyed)
	}
}

func TestPreventDestroyBlocksExplicitDelete(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  files {
    file "/tmp/protected" {
      ensure = "absent"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`))
	provider := NewMemoryProvider()
	address := `host.server1.files.file["/tmp/protected"]`
	provider.Observed[address] = Observed{Exists: true}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	_, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err == nil {
		t.Fatal("plan succeeded, want prevent_destroy error")
	}
	if !strings.Contains(err.Error(), "lifecycle.prevent_destroy") {
		t.Fatalf("error = %v, want prevent_destroy", err)
	}
}

func TestDeleteDiagnosticsForPlanDocument(t *testing.T) {
	tests := []struct {
		name         string
		step         Step
		wantBehavior string
		wantRisk     string
		wantNote     string
	}{
		{
			name: "sysctl remove managed artifact",
			step: Step{
				Address: "host.server1.kernel.sysctl[\"net.ipv4.tcp_congestion_control\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "sysctl",
					Desired: map[string]any{"key": "net.ipv4.tcp_congestion_control", "value": "bbr"},
				},
			},
			wantBehavior: "remove-managed-artifact",
			wantRisk:     "medium",
			wantNote:     "runtime sysctl value is not restored",
		},
		{
			name: "apt source keep forget",
			step: Step{
				Address: "host.server1.apt.source_file[\"main\"]",
				Action:  ActionForget,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "apt_source_file",
					Desired: map[string]any{"path": "/etc/apt/sources.list.d/main.sources", "on_destroy": "keep"},
				},
			},
			wantBehavior: "forget",
			wantRisk:     "low",
			wantNote:     "without modifying the remote resource",
		},
		{
			name: "apt source restore",
			step: Step{
				Address: "host.server1.apt.source_file[\"main\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "apt_source_file",
					Desired: map[string]any{"path": "/etc/apt/sources.list.d/main.sources", "on_destroy": "restore"},
				},
			},
			wantBehavior: "restore-original",
			wantRisk:     "medium",
			wantNote:     "restores the apt source file content",
		},
		{
			name: "directory destructive",
			step: Step{
				Address: "host.server1.directories.directory[\"/srv/app\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "directory",
					Desired: map[string]any{"path": "/srv/app"},
				},
			},
			wantBehavior: "destructive",
			wantRisk:     "high",
			wantNote:     "removes directory recursively",
		},
		{
			name: "systemd unit external side effect",
			step: Step{
				Address: "host.server1.systemd.unit[\"app.service\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "systemd_unit",
					Desired: map[string]any{"path": "/etc/systemd/system/app.service"},
				},
			},
			wantBehavior: "external-side-effect",
			wantRisk:     "high",
			wantNote:     "daemon-reload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Plan{Steps: []Step{tt.step}, Summary: coreplan.Summary{Delete: 1}}.Document(coreplan.Options{})
			if len(doc.Changes) != 1 {
				t.Fatalf("changes = %d, want 1", len(doc.Changes))
			}
			change := doc.Changes[0]
			if change.DeleteBehavior != tt.wantBehavior {
				t.Fatalf("delete behavior = %q, want %q; change=%#v", change.DeleteBehavior, tt.wantBehavior, change)
			}
			if change.DeleteRisk != tt.wantRisk {
				t.Fatalf("delete risk = %q, want %q; change=%#v", change.DeleteRisk, tt.wantRisk, change)
			}
			if !containsSubstring(change.DeleteNotes, tt.wantNote) {
				t.Fatalf("delete notes = %#v, want substring %q", change.DeleteNotes, tt.wantNote)
			}
		})
	}
}

func TestDeleteDiagnosticsOmittedForNonDeleteActions(t *testing.T) {
	step := Step{
		Address: "host.server1.files.file[\"/tmp/example\"]",
		Action:  ActionCreate,
		Node: graph.Node{
			Host:    "server1",
			Kind:    "file",
			Desired: map[string]any{"path": "/tmp/example"},
		},
	}
	doc := Plan{Steps: []Step{step}, Summary: coreplan.Summary{Create: 1}}.Document(coreplan.Options{})
	if len(doc.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(doc.Changes))
	}
	change := doc.Changes[0]
	if change.DeleteBehavior != "" || len(change.DeleteNotes) != 0 || change.DeleteRisk != "" {
		t.Fatalf("non-delete change has delete diagnostics: %#v", change)
	}
}

func fixtureProgramAndGraph(t *testing.T, fixture string) (*ir.Program, *graph.ResourceGraph) {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := graph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return program, resourceGraph
}

func testHostFacts() map[string]ir.HostFacts {
	out := map[string]ir.HostFacts{}
	for _, name := range []string{
		"compose1",
		"docker1",
	} {
		out[name] = ir.HostFacts{System: ir.SystemFacts{
			Hostname:     name,
			Architecture: "amd64",
			Codename:     "trixie",
		}}
	}
	return out
}

func twoFileProgramAndGraph(host string) (*ir.Program, *graph.ResourceGraph) {
	return &ir.Program{Hosts: []ir.HostSpec{{Name: host}}}, &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode(host, "/tmp/a", nil),
		fileNode(host, "/tmp/b", nil),
	}}
}

func fileNode(host, path string, deps []string) graph.Node {
	return graph.Node{
		Host:      host,
		Address:   "host." + host + ".files.file[" + fmt.Sprintf("%q", path) + "]",
		Kind:      "file",
		Summary:   "create file " + path,
		DependsOn: deps,
		Desired: map[string]any{
			"path":    path,
			"content": path,
			"owner":   "root",
			"group":   "root",
			"mode":    "0644",
			"ensure":  "present",
		},
	}
}

func componentScriptModeFixture(mode string) string {
	return fmt.Sprintf(`
component "managed_app" {
  script "reload" {
    mode = %q
    run  = "systemctl reload app.service"
  }

  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }

    file "/etc/app.extra" {
      content   = "managed"
      on_change = script.reload
    }
  }
}

host "app1" {
  component "app" {
    source = component.managed_app
  }
}
`, mode)
}

func cloneCommandMatrix(in [][]string) [][]string {
	out := make([][]string, len(in))
	for i := range in {
		out[i] = append([]string(nil), in[i]...)
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func hasStepAction(plan Plan, address string, action string) bool {
	for _, step := range plan.Steps {
		if step.Address == address && step.Action == action {
			return true
		}
	}
	return false
}

func assertGolden(t *testing.T, golden string, got string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func writeEngineConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}

type planErrorProvider struct {
	*MemoryProvider
}

type testRenewableBackend struct {
	*MemoryBackend
	lock *testRenewableLock
}

type cleanupTestBackend struct {
	*MemoryBackend
	mu          sync.Mutex
	locks       map[string]*cleanupTestLock
	acquireErrs map[string]error
}

type postCancelAcquireBackend struct {
	*MemoryBackend
	slowStarted chan struct{}
	startOnce   sync.Once
	slowLock    *cleanupTestLock
	acquireErr  error
}

func (b *postCancelAcquireBackend) Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	switch host.Name {
	case "failing":
		<-b.slowStarted
		return nil, b.acquireErr
	case "slow":
		b.startOnce.Do(func() { close(b.slowStarted) })
		<-ctx.Done()
		return b.slowLock, nil
	default:
		return nil, fmt.Errorf("unexpected host %q", host.Name)
	}
}

func (b *cleanupTestBackend) Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.acquireErrs[host.Name]; err != nil {
		return nil, err
	}
	lock := b.locks[host.Name]
	if lock == nil {
		return nil, fmt.Errorf("test lock for host %q is not configured", host.Name)
	}
	return lock, nil
}

type cleanupTestLock struct {
	mu              sync.Mutex
	err             error
	waitForDeadline bool
	name            string
	order           *[]string
	orderMu         *sync.Mutex
	calls           int
	sawCanceled     bool
	sawDeadline     bool
}

func (l *cleanupTestLock) Unlock(ctx context.Context) error {
	if l.order != nil {
		l.orderMu.Lock()
		*l.order = append(*l.order, l.name)
		l.orderMu.Unlock()
	}
	l.mu.Lock()
	l.calls++
	if ctx.Err() != nil {
		l.sawCanceled = true
	}
	if _, ok := ctx.Deadline(); ok {
		l.sawDeadline = true
	}
	waitForDeadline := l.waitForDeadline
	err := l.err
	l.mu.Unlock()
	if waitForDeadline {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func (l *cleanupTestLock) snapshot() (calls int, sawCanceled bool, sawDeadline bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls, l.sawCanceled, l.sawDeadline
}

func (b *testRenewableBackend) Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	return b.lock, nil
}

type testRenewableLock struct {
	mu       sync.Mutex
	lost     chan struct{}
	err      error
	unlocked bool
}

func (l *testRenewableLock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.unlocked = true
	return l.err
}

func (l *testRenewableLock) Lost() <-chan struct{} {
	return l.lost
}

func (l *testRenewableLock) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *testRenewableLock) fail(err error) {
	l.mu.Lock()
	l.err = err
	close(l.lost)
	l.mu.Unlock()
}

type leaseBlockingProvider struct {
	*MemoryProvider
	started chan struct{}
	once    sync.Once
}

type afterApplyProvider struct {
	*MemoryProvider
	hook func() error
}

func (p *afterApplyProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	observed, err := p.MemoryProvider.Apply(ctx, step)
	if err != nil {
		return nil, err
	}
	if p.hook != nil {
		if err := p.hook(); err != nil {
			return nil, err
		}
	}
	return observed, nil
}

func (p *leaseBlockingProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p planErrorProvider) Plan(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	return ProviderPlan{}, fmt.Errorf("injected observed read failure")
}

type recordingPayloadProvider struct {
	*MemoryProvider
	Payloads map[string]map[string]any
}

func (p *recordingPayloadProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	if p.Payloads == nil {
		p.Payloads = map[string]map[string]any{}
	}
	p.Payloads[step.Address] = cloneMap(step.Node.ProviderPayload)
	return p.MemoryProvider.Apply(ctx, step)
}

type recordingOperationProvider struct {
	*MemoryProvider
	ScriptPayloads map[string][]graph.ScriptPayload
	OutputSHA      string
}

func (p *recordingOperationProvider) RunOperation(ctx context.Context, operation graph.Operation) (OperationResult, error) {
	if p.ScriptPayloads == nil {
		p.ScriptPayloads = map[string][]graph.ScriptPayload{}
	}
	if operation.ScriptPayload != nil {
		payload := *operation.ScriptPayload
		payload.Interpreter = append([]string(nil), payload.Interpreter...)
		payload.Commands = cloneCommandMatrix(payload.Commands)
		payload.TriggerAddresses = append([]string(nil), payload.TriggerAddresses...)
		payload.TriggerPaths = append([]string(nil), payload.TriggerPaths...)
		p.ScriptPayloads[operation.Address] = append(p.ScriptPayloads[operation.Address], payload)
	}
	result, err := p.MemoryProvider.RunOperation(ctx, operation)
	if err != nil || operation.ScriptPayload == nil || len(operation.ScriptPayload.Outputs) == 0 {
		return result, err
	}
	sha := p.OutputSHA
	if sha == "" {
		sha = "recorded-sha"
	}
	result.Outputs = map[string]map[string]any{}
	for _, output := range operation.ScriptPayload.Outputs {
		result.Outputs[output.Address] = map[string]any{
			"exists": true,
			"is_dir": false,
			"sha256": sha,
			"path":   output.Path,
		}
	}
	return result, nil
}

type concurrencyProvider struct {
	*MemoryProvider
	mu        sync.Mutex
	active    int
	maxActive int
}

func (p *concurrencyProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.MemoryProvider.Apply(ctx, step)
}

type concurrencyBackend struct {
	Backend       Backend
	mu            sync.Mutex
	active        int
	maxReadActive int
}

func (b *concurrencyBackend) Read(ctx context.Context, host ir.HostSpec) (corestate.State, error) {
	b.mu.Lock()
	b.active++
	if b.active > b.maxReadActive {
		b.maxReadActive = b.active
	}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.active--
		b.mu.Unlock()
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return corestate.State{}, ctx.Err()
	}
	return b.Backend.Read(ctx, host)
}

func (b *concurrencyBackend) Write(ctx context.Context, host ir.HostSpec, st corestate.State) error {
	return b.Backend.Write(ctx, host, st)
}

func (b *concurrencyBackend) Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	return b.Backend.Lock(ctx, host, timeout)
}

type hostPlanningConcurrencyProvider struct {
	*MemoryProvider
	mu                sync.Mutex
	active            int
	maxPlanHostActive int
}

func (p *hostPlanningConcurrencyProvider) PlanHost(ctx context.Context, host ir.HostSpec, nodes []graph.Node, priors map[string]*corestate.Resource) (map[string]ProviderPlan, error) {
	p.mu.Lock()
	p.active++
	if p.active > p.maxPlanHostActive {
		p.maxPlanHostActive = p.active
	}
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := make(map[string]ProviderPlan, len(nodes))
	for _, node := range nodes {
		out[node.Address] = Compare(node, priors[node.Address], Observed{Exists: false})
	}
	return out, nil
}
