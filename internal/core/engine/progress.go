package engine

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/mofelee/debianform/internal/core/termstyle"
)

const defaultProgressInterval = 15 * time.Second

type progressLogger struct {
	w        io.Writer
	interval time.Duration
	style    termstyle.Options
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
	stop    sync.Once
	log     sync.Once
}

func newProgressLogger(w io.Writer) *progressLogger {
	return newProgressLoggerWithStyle(w, termstyle.Options{})
}

func newProgressLoggerWithStyle(w io.Writer, style termstyle.Options) *progressLogger {
	if w == nil {
		return nil
	}
	return &progressLogger{w: w, interval: defaultProgressInterval, style: style}
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
	p.Logf("%s", task.line("start", action, 0))
	go task.heartbeat()
	return task
}

func (t *progressTask) Done(err error) {
	if t == nil || t.logger == nil {
		return
	}
	t.log.Do(func() {
		t.stopHeartbeat()
		status := "done"
		if err != nil {
			status = "failed"
		}
		t.logger.Logf("%s", t.line(status, t.action, time.Since(t.started)))
	})
}

func (t *progressTask) stopHeartbeat() {
	if t == nil {
		return
	}
	t.stop.Do(func() {
		close(t.done)
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
			t.logger.Logf("%s", t.line("still", t.action, time.Since(t.started)))
			timer.Reset(interval)
		case <-t.done:
			return
		}
	}
}

func (t *progressTask) line(status string, action string, elapsed time.Duration) string {
	if t.logger != nil && t.logger.style.Color {
		return t.styledLine(status, action, elapsed)
	}
	var parts []string
	if t.host != "" {
		parts = append(parts, t.host+":")
	}
	parts = append(parts, status+" "+action)
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

func (t *progressTask) styledLine(status string, action string, elapsed time.Duration) string {
	style := t.logger.style
	parts := []string{progressStatusBadge(status, style)}
	if t.host != "" {
		parts = append(parts, termstyle.Apply(t.host+":", style, termstyle.Bold, termstyle.Cyan))
	}
	parts = append(parts, progressActionText(action, style))
	if t.subject != "" {
		parts = append(parts, termstyle.Apply(oneLineProgressText(t.subject), style, termstyle.Gray))
	}
	if t.summary != "" {
		parts = append(parts, termstyle.Apply("- "+oneLineProgressText(t.summary), style, termstyle.Gray))
	}
	if elapsed > 0 {
		parts = append(parts, termstyle.Apply("("+formatProgressDuration(elapsed)+")", style, termstyle.Gray))
	}
	return strings.Join(parts, " ")
}

func progressStatusBadge(status string, style termstyle.Options) string {
	label := strings.ToUpper(status)
	if style.Unicode {
		switch status {
		case "start":
			label = "▶ " + label
		case "done":
			label = "✓ " + label
		case "still":
			label = "… " + label
		case "failed":
			label = "✕ " + label
		}
	}
	switch status {
	case "start":
		return termstyle.Badge(label, style, termstyle.Blue, termstyle.BgBlue)
	case "done":
		return termstyle.Badge(label, style, termstyle.Green, termstyle.BgGreen)
	case "still":
		return termstyle.Badge(label, style, termstyle.Gray, termstyle.BgGray)
	case "failed":
		return termstyle.Badge(label, style, termstyle.Red, termstyle.BgRed)
	default:
		return termstyle.Badge(label, style, termstyle.Gray, termstyle.BgGray)
	}
}

func progressActionText(action string, style termstyle.Options) string {
	color := termstyle.ActionColor(action)
	if color == "" {
		color = termstyle.Bold
	}
	return termstyle.Apply(action, style, color)
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
