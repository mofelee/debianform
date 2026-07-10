package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestCheckHoldsEverySelectedHostLockAndStaysReadOnly(t *testing.T) {
	hosts := []ir.HostSpec{
		{Name: "server1", Facts: ir.HostFacts{System: ir.SystemFacts{Hostname: "server1"}}},
		{Name: "server2", Facts: ir.HostFacts{System: ir.SystemFacts{Hostname: "server2"}}},
	}
	node1 := fileNode("server1", "/tmp/server1", nil)
	node2 := fileNode("server2", "/tmp/server2", nil)
	resourceGraph := &graph.ResourceGraph{
		Nodes: []graph.Node{node1, node2},
		Operations: []graph.Operation{{
			Host:        "server1",
			Address:     "host.server1.operations.after_check",
			TriggeredBy: []string{node1.Address},
		}},
	}
	tracker := newCheckLockTracker(hosts)
	backend := &checkTrackingBackend{tracker: tracker}
	provider := &checkTrackingProvider{tracker: tracker}
	engine := Engine{Backend: backend, Provider: provider}
	lockTimeout := 321 * time.Millisecond

	plan, err := engine.Check(context.Background(), &ir.Program{Hosts: hosts}, resourceGraph, Options{
		Parallel:    2,
		LockTimeout: lockTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || len(plan.Operations) != 1 {
		t.Fatalf("check plan: steps=%d operations=%d, want 2/1", len(plan.Steps), len(plan.Operations))
	}

	snapshot := tracker.snapshot()
	for _, host := range hosts {
		if got := snapshot.lockTimeouts[host.Name]; got != lockTimeout {
			t.Errorf("host %s lock timeout = %s, want %s", host.Name, got, lockTimeout)
		}
		if got := snapshot.reads[host.Name]; got != 1 {
			t.Errorf("host %s state reads = %d, want 1", host.Name, got)
		}
		if got := snapshot.unlocks[host.Name]; got != 1 {
			t.Errorf("host %s unlocks = %d, want 1", host.Name, got)
		}
	}
	for _, node := range resourceGraph.Nodes {
		if got := snapshot.plans[node.Address]; got != 1 {
			t.Errorf("resource %s inspections = %d, want 1", node.Address, got)
		}
	}
	if len(snapshot.locked) != 0 {
		t.Errorf("locks still held after check: %v", snapshot.locked)
	}
	if snapshot.writes != 0 {
		t.Errorf("state writes = %d, want 0", snapshot.writes)
	}
	if snapshot.applies != 0 || snapshot.destroys != 0 || snapshot.operations != 0 {
		t.Errorf("provider mutations: apply=%d destroy=%d operation=%d, want all zero", snapshot.applies, snapshot.destroys, snapshot.operations)
	}
}

func TestPlanRemainsUnlocked(t *testing.T) {
	backend := &checkNoLockBackend{MemoryBackend: NewMemoryBackend()}
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/unlocked-plan", nil)}}

	if _, err := engine.Plan(context.Background(), program, resourceGraph, Options{LockTimeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	lockCalls := backend.lockCalls
	backend.mu.Unlock()
	if lockCalls != 0 {
		t.Fatalf("plan lock calls = %d, want 0", lockCalls)
	}
}

func TestCheckWaitsForHeldLockThenSucceeds(t *testing.T) {
	backend := newCheckGateBackend()
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/wait-for-lock", nil)}}
	type result struct {
		err error
	}
	resultCh := make(chan result, 1)

	go func() {
		_, err := engine.Check(context.Background(), program, resourceGraph, Options{LockTimeout: time.Second})
		resultCh <- result{err: err}
	}()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("check did not start lock acquisition")
	}
	select {
	case got := <-resultCh:
		t.Fatalf("check returned while lock was held: %v", got.err)
	default:
	}
	close(backend.release)

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("check after lock release: %v", got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("check did not finish after lock release")
	}
	if got := backend.observedTimeout(); got != time.Second {
		t.Fatalf("lock timeout = %s, want 1s", got)
	}
}

func TestCheckFailsWhenLockTimeoutExpires(t *testing.T) {
	backend := newCheckGateBackend()
	defer close(backend.release)
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/lock-timeout", nil)}}
	lockTimeout := 25 * time.Millisecond

	_, err := engine.Check(context.Background(), program, resourceGraph, Options{LockTimeout: lockTimeout})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("check error = %v, want lock deadline exceeded", err)
	}
	if got := backend.observedTimeout(); got != lockTimeout {
		t.Fatalf("lock timeout = %s, want %s", got, lockTimeout)
	}
	if reads := backend.observedReads(); reads != 0 {
		t.Fatalf("state reads after lock timeout = %d, want 0", reads)
	}
}

func TestCheckReturnsUnlockErrorAfterSuccessfulPlan(t *testing.T) {
	unlockErr := errors.New("injected check unlock failure")
	lock := &cleanupTestLock{err: unlockErr}
	backend := &cleanupTestBackend{
		MemoryBackend: NewMemoryBackend(),
		locks:         map[string]*cleanupTestLock{"server1": lock},
	}
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/check-unlock-error", nil)}}

	plan, err := engine.Check(context.Background(), program, resourceGraph, Options{})
	if !errors.Is(err, unlockErr) {
		t.Fatalf("check error = %v, want unlock failure", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("check steps = %d, want successful plan with 1 step", len(plan.Steps))
	}
	if calls, _, _ := lock.snapshot(); calls != 1 {
		t.Fatalf("unlock calls = %d, want 1", calls)
	}
	if !strings.Contains(err.Error(), `unlock state for host "server1"`) {
		t.Fatalf("check error lacks host context: %v", err)
	}
}

func TestCheckPlanningFailureReleasesEveryLockAndJoinsCleanupErrors(t *testing.T) {
	planErr := errors.New("injected check planning failure")
	cleanupErr1 := errors.New("server1 check cleanup failed")
	cleanupErr2 := errors.New("server2 check cleanup failed")
	locks := map[string]*cleanupTestLock{
		"server1": {err: cleanupErr1},
		"server2": {err: cleanupErr2},
	}
	backend := &cleanupTestBackend{MemoryBackend: NewMemoryBackend(), locks: locks}
	provider := &checkFailingProvider{err: planErr}
	engine := Engine{Backend: backend, Provider: provider}
	hosts := []ir.HostSpec{{Name: "server1"}, {Name: "server2"}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/check-plan-error", nil)}}

	_, err := engine.Check(context.Background(), &ir.Program{Hosts: hosts}, resourceGraph, Options{Parallel: 1})
	for _, want := range []error{planErr, cleanupErr1, cleanupErr2} {
		if !errors.Is(err, want) {
			t.Errorf("check error = %v, want joined error %v", err, want)
		}
	}
	for _, host := range hosts {
		if calls, _, _ := locks[host.Name].snapshot(); calls != 1 {
			t.Errorf("host %s unlock calls = %d, want 1", host.Name, calls)
		}
	}
}

func TestCheckCancelsInspectionAndReturnsLeaseLossCause(t *testing.T) {
	leaseCause := errors.New("injected check lease renewal failure")
	lease := &testRenewableLock{lost: make(chan struct{})}
	backend := &testRenewableBackend{MemoryBackend: NewMemoryBackend(), lock: lease}
	provider := &checkLeaseBlockingProvider{started: make(chan struct{})}
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/check-lease-loss", nil)}}
	resultCh := make(chan error, 1)

	go func() {
		_, err := engine.Check(context.Background(), program, resourceGraph, Options{})
		resultCh <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider inspection did not start")
	}
	lease.fail(leaseCause)

	select {
	case err := <-resultCh:
		if !errors.Is(err, leaseCause) {
			t.Fatalf("check error = %v, want lease renewal root cause", err)
		}
		if count := strings.Count(err.Error(), leaseCause.Error()); count != 1 {
			t.Fatalf("lease renewal root cause appears %d times in error: %v", count, err)
		}
	case <-time.After(time.Second):
		t.Fatal("check did not stop after lease loss")
	}
	lease.mu.Lock()
	unlocked := lease.unlocked
	lease.mu.Unlock()
	if !unlocked {
		t.Fatal("lease was not unlocked after renewal failure")
	}
	if calls := provider.observedMutationCalls(); calls != 0 {
		t.Fatalf("provider mutation calls = %d, want 0", calls)
	}
}

type checkLockTracker struct {
	mu           sync.Mutex
	expected     []string
	locked       map[string]bool
	lockTimeouts map[string]time.Duration
	reads        map[string]int
	unlocks      map[string]int
	plans        map[string]int
	writes       int
	applies      int
	destroys     int
	operations   int
}

type checkNoLockBackend struct {
	*MemoryBackend
	mu        sync.Mutex
	lockCalls int
}

func (b *checkNoLockBackend) Lock(context.Context, ir.HostSpec, time.Duration) (Lock, error) {
	b.mu.Lock()
	b.lockCalls++
	b.mu.Unlock()
	return nil, fmt.Errorf("plan unexpectedly acquired a state lock")
}

type checkLockTrackerSnapshot struct {
	locked       map[string]bool
	lockTimeouts map[string]time.Duration
	reads        map[string]int
	unlocks      map[string]int
	plans        map[string]int
	writes       int
	applies      int
	destroys     int
	operations   int
}

func newCheckLockTracker(hosts []ir.HostSpec) *checkLockTracker {
	expected := make([]string, 0, len(hosts))
	for _, host := range hosts {
		expected = append(expected, host.Name)
	}
	return &checkLockTracker{
		expected:     expected,
		locked:       map[string]bool{},
		lockTimeouts: map[string]time.Duration{},
		reads:        map[string]int{},
		unlocks:      map[string]int{},
		plans:        map[string]int{},
	}
}

func (t *checkLockTracker) allLockedError() error {
	for _, host := range t.expected {
		if !t.locked[host] {
			return fmt.Errorf("host %s is not locked", host)
		}
	}
	return nil
}

func (t *checkLockTracker) snapshot() checkLockTrackerSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return checkLockTrackerSnapshot{
		locked:       cloneBoolMap(t.locked),
		lockTimeouts: cloneDurationMap(t.lockTimeouts),
		reads:        cloneIntMap(t.reads),
		unlocks:      cloneIntMap(t.unlocks),
		plans:        cloneIntMap(t.plans),
		writes:       t.writes,
		applies:      t.applies,
		destroys:     t.destroys,
		operations:   t.operations,
	}
}

type checkTrackingBackend struct {
	tracker *checkLockTracker
}

func (b *checkTrackingBackend) Read(_ context.Context, host ir.HostSpec) (corestate.State, error) {
	b.tracker.mu.Lock()
	defer b.tracker.mu.Unlock()
	b.tracker.reads[host.Name]++
	if err := b.tracker.allLockedError(); err != nil {
		return corestate.State{}, fmt.Errorf("read state for %s: %w", host.Name, err)
	}
	return corestate.Empty(host.Name), nil
}

func (b *checkTrackingBackend) Write(_ context.Context, _ ir.HostSpec, _ corestate.State) (corestate.State, error) {
	b.tracker.mu.Lock()
	b.tracker.writes++
	b.tracker.mu.Unlock()
	return corestate.State{}, fmt.Errorf("check unexpectedly wrote state")
}

func (b *checkTrackingBackend) Lock(_ context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	b.tracker.mu.Lock()
	defer b.tracker.mu.Unlock()
	if b.tracker.locked[host.Name] {
		return nil, fmt.Errorf("state for host %s is already locked", host.Name)
	}
	b.tracker.locked[host.Name] = true
	b.tracker.lockTimeouts[host.Name] = timeout
	return &checkTrackingLock{tracker: b.tracker, host: host.Name}, nil
}

type checkTrackingLock struct {
	tracker *checkLockTracker
	host    string
}

func (l *checkTrackingLock) Unlock(_ context.Context) error {
	l.tracker.mu.Lock()
	defer l.tracker.mu.Unlock()
	l.tracker.unlocks[l.host]++
	delete(l.tracker.locked, l.host)
	return nil
}

type checkTrackingProvider struct {
	tracker *checkLockTracker
}

func (p *checkTrackingProvider) Plan(_ context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	p.tracker.mu.Lock()
	defer p.tracker.mu.Unlock()
	p.tracker.plans[node.Address]++
	if err := p.tracker.allLockedError(); err != nil {
		return ProviderPlan{}, fmt.Errorf("inspect %s: %w", node.Address, err)
	}
	return Compare(node, prior, Observed{Exists: false}), nil
}

func (p *checkTrackingProvider) Apply(_ context.Context, _ Step) (map[string]any, error) {
	p.tracker.mu.Lock()
	p.tracker.applies++
	p.tracker.mu.Unlock()
	return nil, fmt.Errorf("check unexpectedly applied a resource")
}

func (p *checkTrackingProvider) Destroy(_ context.Context, _ Step) error {
	p.tracker.mu.Lock()
	p.tracker.destroys++
	p.tracker.mu.Unlock()
	return fmt.Errorf("check unexpectedly destroyed a resource")
}

func (p *checkTrackingProvider) RunOperation(_ context.Context, _ graph.Operation) (OperationResult, error) {
	p.tracker.mu.Lock()
	p.tracker.operations++
	p.tracker.mu.Unlock()
	return OperationResult{}, fmt.Errorf("check unexpectedly ran an operation")
}

type checkGateBackend struct {
	inner   *MemoryBackend
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu      sync.Mutex
	timeout time.Duration
	reads   int
}

func newCheckGateBackend() *checkGateBackend {
	return &checkGateBackend{
		inner:   NewMemoryBackend(),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *checkGateBackend) Read(ctx context.Context, host ir.HostSpec) (corestate.State, error) {
	b.mu.Lock()
	b.reads++
	b.mu.Unlock()
	return b.inner.Read(ctx, host)
}

func (b *checkGateBackend) Write(ctx context.Context, host ir.HostSpec, st corestate.State) (corestate.State, error) {
	return b.inner.Write(ctx, host, st)
}

func (b *checkGateBackend) Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	b.mu.Lock()
	b.timeout = timeout
	b.mu.Unlock()
	b.once.Do(func() { close(b.started) })
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-b.release:
		return b.inner.Lock(ctx, host, timeout)
	case <-timer.C:
		return nil, context.DeadlineExceeded
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *checkGateBackend) observedTimeout() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.timeout
}

func (b *checkGateBackend) observedReads() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reads
}

type checkFailingProvider struct {
	err error
}

func (p *checkFailingProvider) Plan(context.Context, graph.Node, *corestate.Resource) (ProviderPlan, error) {
	return ProviderPlan{}, p.err
}

func (p *checkFailingProvider) Apply(context.Context, Step) (map[string]any, error) {
	return nil, fmt.Errorf("unexpected apply")
}

func (p *checkFailingProvider) Destroy(context.Context, Step) error {
	return fmt.Errorf("unexpected destroy")
}

func (p *checkFailingProvider) RunOperation(context.Context, graph.Operation) (OperationResult, error) {
	return OperationResult{}, fmt.Errorf("unexpected operation")
}

type checkLeaseBlockingProvider struct {
	started chan struct{}
	once    sync.Once

	mu            sync.Mutex
	mutationCalls int
}

func (p *checkLeaseBlockingProvider) Plan(ctx context.Context, _ graph.Node, _ *corestate.Resource) (ProviderPlan, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return ProviderPlan{}, ctx.Err()
}

func (p *checkLeaseBlockingProvider) Apply(context.Context, Step) (map[string]any, error) {
	p.recordMutation()
	return nil, fmt.Errorf("unexpected apply")
}

func (p *checkLeaseBlockingProvider) Destroy(context.Context, Step) error {
	p.recordMutation()
	return fmt.Errorf("unexpected destroy")
}

func (p *checkLeaseBlockingProvider) RunOperation(context.Context, graph.Operation) (OperationResult, error) {
	p.recordMutation()
	return OperationResult{}, fmt.Errorf("unexpected operation")
}

func (p *checkLeaseBlockingProvider) recordMutation() {
	p.mu.Lock()
	p.mutationCalls++
	p.mu.Unlock()
}

func (p *checkLeaseBlockingProvider) observedMutationCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mutationCalls
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneDurationMap(in map[string]time.Duration) map[string]time.Duration {
	out := make(map[string]time.Duration, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
