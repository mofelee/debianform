package engine

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const defaultProgressInterval = 15 * time.Second

type progressLogger struct {
	w        io.Writer
	interval time.Duration
	mu       sync.Mutex
}

type progressTask struct {
	logger  *progressLogger
	action  string
	subject string
	host    string
	summary string
	started time.Time
	done    chan struct{}
	once    sync.Once
}

func newProgressLogger(w io.Writer) *progressLogger {
	if w == nil {
		return nil
	}
	return &progressLogger{w: w, interval: defaultProgressInterval}
}

func (p *progressLogger) Logf(format string, args ...any) {
	if p == nil || p.w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "dbf: "+format+"\n", args...)
}

func (p *progressLogger) Start(host, action, subject, summary string) *progressTask {
	if p == nil || p.w == nil {
		return nil
	}
	task := &progressTask{
		logger:  p,
		action:  action,
		subject: subject,
		host:    host,
		summary: summary,
		started: time.Now(),
		done:    make(chan struct{}),
	}
	p.Logf("%s", task.line("start "+action, 0))
	go task.heartbeat()
	return task
}

func (t *progressTask) Done(err error) {
	if t == nil || t.logger == nil {
		return
	}
	t.once.Do(func() {
		close(t.done)
		status := "done " + t.action
		if err != nil {
			status = "failed " + t.action
		}
		t.logger.Logf("%s", t.line(status, time.Since(t.started)))
	})
}

func (t *progressTask) heartbeat() {
	interval := t.logger.interval
	if interval <= 0 {
		interval = defaultProgressInterval
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			t.logger.Logf("%s", t.line("still "+t.action, time.Since(t.started)))
			timer.Reset(interval)
		case <-t.done:
			return
		}
	}
}

func (t *progressTask) line(action string, elapsed time.Duration) string {
	var parts []string
	if t.host != "" {
		parts = append(parts, t.host+":")
	}
	parts = append(parts, action)
	if t.subject != "" {
		parts = append(parts, oneLineProgressText(t.subject))
	}
	if t.summary != "" {
		parts = append(parts, "- "+oneLineProgressText(t.summary))
	}
	if elapsed > 0 {
		parts = append(parts, "("+formatProgressDuration(elapsed)+")")
	}
	return strings.Join(parts, " ")
}

func oneLineProgressText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func formatProgressDuration(value time.Duration) string {
	if value < time.Second {
		return value.Round(time.Millisecond).String()
	}
	return value.Round(time.Second).String()
}
