package plan

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/mofelee/debianform/internal/v2/graph"
	"github.com/mofelee/debianform/internal/v2/ir"
)

const FormatVersion = "debianform.plan.v2alpha1"

type Options struct {
	CommandFile string
	Host        string
	Debug       bool
	Now         func() time.Time
}

type Document struct {
	FormatVersion string          `json:"format_version"`
	GeneratedAt   string          `json:"generated_at"`
	Command       Command         `json:"command"`
	Summary       Summary         `json:"summary"`
	Changes       []Change        `json:"changes"`
	Operations    []OperationNode `json:"operations"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
}

type Command struct {
	File string `json:"file,omitempty"`
	Host string `json:"host,omitempty"`
}

type Summary struct {
	Create     int `json:"create"`
	Update     int `json:"update"`
	Delete     int `json:"delete"`
	NoOp       int `json:"no_op"`
	Operations int `json:"operations"`
}

type Change struct {
	Address         string       `json:"address"`
	Action          string       `json:"action"`
	Summary         string       `json:"summary"`
	Source          ir.SourceRef `json:"source"`
	ProviderAddress string       `json:"provider_address,omitempty"`
	Diff            DiffNode     `json:"diff"`
	LowLevelActions []string     `json:"low_level_actions,omitempty"`
}

type DiffNode struct {
	Path          []string       `json:"path"`
	Kind          string         `json:"kind"`
	Action        string         `json:"action"`
	Sensitive     bool           `json:"sensitive"`
	Before        any            `json:"before,omitempty"`
	After         any            `json:"after,omitempty"`
	BeforeSummary map[string]any `json:"before_summary,omitempty"`
	AfterSummary  map[string]any `json:"after_summary,omitempty"`
	Children      []DiffNode     `json:"children,omitempty"`
	Hunks         []TextHunk     `json:"hunks,omitempty"`
}

type TextHunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

type OperationNode struct {
	Address        string       `json:"address"`
	Action         string       `json:"action"`
	Summary        string       `json:"summary"`
	DependsOn      []string     `json:"depends_on,omitempty"`
	TriggeredBy    []string     `json:"triggered_by,omitempty"`
	CommandPreview string       `json:"command_preview,omitempty"`
	Source         ir.SourceRef `json:"source,omitempty"`
}

type Diagnostic struct {
	Severity string       `json:"severity"`
	Message  string       `json:"message"`
	Source   ir.SourceRef `json:"source,omitempty"`
}

func New(resourceGraph *graph.ResourceGraph, opts Options) Document {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	nodes := append([]graph.Node(nil), resourceGraph.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Address < nodes[j].Address
	})

	changes := make([]Change, 0, len(nodes))
	for _, node := range nodes {
		change := Change{
			Address: node.Address,
			Action:  "create",
			Summary: node.Summary,
			Source:  node.Source,
			Diff:    diffForNode(node),
		}
		if opts.Debug {
			change.ProviderAddress = node.ProviderAddress
		}
		changes = append(changes, change)
	}

	operations := make([]OperationNode, 0, len(resourceGraph.Operations))
	for _, operation := range resourceGraph.Operations {
		operations = append(operations, OperationNode{
			Address:        operation.Address,
			Action:         operation.Action,
			Summary:        operation.Summary,
			DependsOn:      operation.DependsOn,
			TriggeredBy:    operation.TriggeredBy,
			CommandPreview: operation.CommandPreview,
			Source:         operation.Source,
		})
	}

	return Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   now().UTC().Format(time.RFC3339),
		Command: Command{
			File: opts.CommandFile,
			Host: opts.Host,
		},
		Summary: Summary{
			Create:     len(changes),
			Update:     0,
			Delete:     0,
			NoOp:       0,
			Operations: len(operations),
		},
		Changes:     changes,
		Operations:  operations,
		Diagnostics: []Diagnostic{},
	}
}

func diffForNode(node graph.Node) DiffNode {
	return BuildDiff("create", nil, node.Desired)
}

func PrintText(w io.Writer, doc Document) {
	fmt.Fprintln(w, "Plan:")
	if len(doc.Changes) == 0 && len(doc.Operations) == 0 {
		fmt.Fprintln(w, "  No changes.")
		fmt.Fprintln(w)
		printSummary(w, doc.Summary)
		return
	}
	for _, change := range doc.Changes {
		fmt.Fprintf(w, "  %s %s\n", actionSymbol(change.Action), change.Address)
		if change.Summary != "" {
			fmt.Fprintf(w, "    %s\n", change.Summary)
		}
		if source := sourceText(change.Source); source != "" {
			fmt.Fprintf(w, "    source: %s\n", source)
		}
		if change.ProviderAddress != "" {
			fmt.Fprintf(w, "    provider: %s\n", change.ProviderAddress)
		}
		printDiffChildren(w, change.Diff.Children, 4)
	}
	for _, op := range doc.Operations {
		fmt.Fprintf(w, "  ! %s\n", op.Address)
		if op.Summary != "" {
			fmt.Fprintf(w, "    %s\n", op.Summary)
		}
		if len(op.TriggeredBy) > 0 {
			fmt.Fprintf(w, "    triggered_by: %s\n", strings.Join(op.TriggeredBy, ", "))
		}
	}
	fmt.Fprintln(w)
	printSummary(w, doc.Summary)
}

func PrintJSON(w io.Writer, doc Document) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(doc)
}

func PrintHTML(w io.Writer, doc Document) error {
	tmpl, err := template.New("plan").Funcs(template.FuncMap{
		"sourceText": sourceText,
		"hostText":   hostFromAddress,
		"diffText":   diffText,
		"actionText": func(action string) string {
			if action == "" {
				return "unknown"
			}
			return action
		},
	}).Parse(planHTMLTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, htmlView{
		Document: doc,
		Hosts:    collectHosts(doc),
		Actions:  collectActions(doc),
	})
}

func actionSymbol(action string) string {
	switch action {
	case "create", "adopt":
		return "+"
	case "update":
		return "~"
	case "delete", "destroy", "forget":
		return "-"
	case "no-op":
		return "="
	default:
		return "?"
	}
}

func printSummary(w io.Writer, summary Summary) {
	fmt.Fprintf(w, "Summary: %d create, %d update, %d delete, %d no-op, %d operations\n",
		summary.Create,
		summary.Update,
		summary.Delete,
		summary.NoOp,
		summary.Operations,
	)
}

func printDiffChildren(w io.Writer, children []DiffNode, indent int) {
	for _, child := range children {
		printDiffNode(w, child, indent)
	}
}

func printDiffNode(w io.Writer, node DiffNode, indent int) {
	padding := strings.Repeat(" ", indent)
	label := diffPathLabel(node.Path)
	switch node.Kind {
	case "object", "map", "list", "set":
		fmt.Fprintf(w, "%s%s %s\n", padding, actionSymbol(node.Action), label)
		printDiffChildren(w, node.Children, indent+2)
	case "text":
		fmt.Fprintf(w, "%s%s %s\n", padding, actionSymbol(node.Action), label)
		for _, hunk := range node.Hunks {
			fmt.Fprintf(w, "%s  @@ -%d,%d +%d,%d @@\n", padding, hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
			for _, line := range hunk.Lines {
				fmt.Fprintf(w, "%s  %s %s\n", padding, actionSymbol(line.Op), line.Text)
			}
		}
	case "sensitive":
		fmt.Fprintf(w, "%s%s %s: %s\n", padding, actionSymbol(node.Action), label, sensitiveSummaryText(node))
	default:
		fmt.Fprintf(w, "%s%s %s%s\n", padding, actionSymbol(node.Action), label, scalarDiffText(node))
	}
}

func diffText(node DiffNode) string {
	var out strings.Builder
	printDiffChildren(&out, node.Children, 0)
	return strings.TrimRight(out.String(), "\n")
}

func diffPathLabel(path []string) string {
	if len(path) == 0 {
		return "value"
	}
	return strings.Join(path, ".")
}

func scalarDiffText(node DiffNode) string {
	switch node.Action {
	case "create":
		return ": " + formatDiffValue(node.After)
	case "delete":
		return ": " + formatDiffValue(node.Before)
	default:
		return ": " + formatDiffValue(node.Before) + " -> " + formatDiffValue(node.After)
	}
}

func formatDiffValue(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func sensitiveSummaryText(node DiffNode) string {
	switch node.Action {
	case "create":
		return "<sensitive " + summaryText(node.AfterSummary) + ">"
	case "delete":
		return "<sensitive " + summaryText(node.BeforeSummary) + ">"
	default:
		return "<sensitive " + summaryText(node.BeforeSummary) + " -> " + summaryText(node.AfterSummary) + ">"
	}
}

func summaryText(summary map[string]any) string {
	if len(summary) == 0 {
		return "changed"
	}
	parts := []string{}
	if sha, ok := summary["sha256"]; ok {
		parts = append(parts, "sha256="+fmt.Sprint(sha))
	}
	if size, ok := summary["bytes"]; ok {
		parts = append(parts, fmt.Sprint(size)+" bytes")
	}
	if len(parts) == 0 {
		return "changed"
	}
	return strings.Join(parts, ", ")
}

func sourceText(source ir.SourceRef) string {
	if source.File == "" {
		return ""
	}
	if source.Path == "" {
		return fmt.Sprintf("%s:%d", source.File, source.Line)
	}
	return fmt.Sprintf("%s:%d:%s", source.File, source.Line, source.Path)
}

type htmlView struct {
	Document
	Hosts   []string
	Actions []string
}

func collectHosts(doc Document) []string {
	seen := map[string]struct{}{}
	for _, change := range doc.Changes {
		if host := hostFromAddress(change.Address); host != "" {
			seen[host] = struct{}{}
		}
	}
	for _, op := range doc.Operations {
		if host := hostFromAddress(op.Address); host != "" {
			seen[host] = struct{}{}
		}
	}
	return sortedSet(seen)
}

func collectActions(doc Document) []string {
	seen := map[string]struct{}{}
	for _, change := range doc.Changes {
		if change.Action != "" {
			seen[change.Action] = struct{}{}
		}
	}
	for _, op := range doc.Operations {
		if op.Action != "" {
			seen[op.Action] = struct{}{}
		}
	}
	return sortedSet(seen)
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func hostFromAddress(address string) string {
	if !strings.HasPrefix(address, "host.") {
		return ""
	}
	rest := strings.TrimPrefix(address, "host.")
	if idx := strings.Index(rest, "."); idx >= 0 {
		return rest[:idx]
	}
	return ""
}

const planHTMLTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>DebianForm Plan</title>
  <style>
    :root { color-scheme: light dark; --border: #d0d7de; --muted: #57606a; --bg: #ffffff; --panel: #f6f8fa; --text: #24292f; --create: #1a7f37; --update: #9a6700; --delete: #cf222e; --run: #0969da; }
    @media (prefers-color-scheme: dark) { :root { --border: #30363d; --muted: #8b949e; --bg: #0d1117; --panel: #161b22; --text: #e6edf3; --create: #3fb950; --update: #d29922; --delete: #f85149; --run: #58a6ff; } }
    * { box-sizing: border-box; }
    body { margin: 0; background: var(--bg); color: var(--text); font: 14px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    header { padding: 24px 32px 16px; border-bottom: 1px solid var(--border); background: var(--panel); }
    main { padding: 24px 32px 40px; max-width: 1200px; }
    h1 { margin: 0 0 8px; font-size: 24px; line-height: 1.2; }
    h2 { margin: 28px 0 12px; font-size: 18px; }
    .meta, .source { color: var(--muted); }
    .summary { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
    .filters { display: flex; flex-wrap: wrap; gap: 12px; margin: 20px 0 4px; align-items: end; }
    label { display: grid; gap: 4px; color: var(--muted); font-size: 12px; font-weight: 600; text-transform: uppercase; }
    input, select { min-height: 34px; min-width: 180px; border: 1px solid var(--border); border-radius: 6px; padding: 6px 8px; background: var(--bg); color: var(--text); font: inherit; }
    input[type="search"] { min-width: min(420px, 80vw); }
    .pill { border: 1px solid var(--border); border-radius: 999px; padding: 4px 10px; background: var(--bg); }
    table { width: 100%; border-collapse: collapse; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
    th, td { padding: 10px 12px; border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; }
    th { background: var(--panel); font-weight: 600; }
    tr:last-child td { border-bottom: 0; }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 13px; }
    details { margin-top: 8px; }
    summary { cursor: pointer; color: var(--muted); }
    pre { margin: 8px 0 0; padding: 10px; overflow: auto; border: 1px solid var(--border); border-radius: 6px; background: var(--panel); font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; white-space: pre-wrap; }
    .action { font-weight: 700; text-transform: uppercase; white-space: nowrap; }
    .action-create, .action-adopt { color: var(--create); }
    .action-update { color: var(--update); }
    .action-delete, .action-destroy, .action-forget { color: var(--delete); }
    .action-run { color: var(--run); }
    .empty { padding: 18px; border: 1px solid var(--border); border-radius: 6px; background: var(--panel); color: var(--muted); }
    .hidden { display: none; }
  </style>
</head>
<body>
  <header>
    <h1>DebianForm Plan</h1>
    <div class="meta">format {{.FormatVersion}} · generated {{.GeneratedAt}}{{if .Command.File}} · {{.Command.File}}{{end}}{{if .Command.Host}} · host {{.Command.Host}}{{end}}</div>
    <div class="summary">
      <span class="pill">{{.Summary.Create}} create</span>
      <span class="pill">{{.Summary.Update}} update</span>
      <span class="pill">{{.Summary.Delete}} delete</span>
      <span class="pill">{{.Summary.NoOp}} no-op</span>
      <span class="pill">{{.Summary.Operations}} operations</span>
    </div>
  </header>
  <main>
    <div class="filters">
      <label>Action
        <select id="action-filter">
          <option value="">All actions</option>
          {{range .Actions}}<option value="{{.}}">{{.}}</option>{{end}}
        </select>
      </label>
      <label>Host
        <select id="host-filter">
          <option value="">All hosts</option>
          {{range .Hosts}}<option value="{{.}}">{{.}}</option>{{end}}
        </select>
      </label>
      <label>Search
        <input id="search-filter" type="search" placeholder="Address, summary, command, source">
      </label>
    </div>

    <h2>Changes</h2>
    {{if .Changes}}
    <table>
      <thead><tr><th>Action</th><th>Address</th><th>Summary</th><th>Source</th></tr></thead>
      <tbody>
      {{range .Changes}}
        <tr data-plan-row data-action="{{.Action}}" data-host="{{hostText .Address}}" data-search="{{.Address}} {{.Summary}} {{sourceText .Source}}">
          <td><span class="action action-{{.Action}}">{{actionText .Action}}</span></td>
          <td><code>{{.Address}}</code></td>
          <td>{{.Summary}}{{if .ProviderAddress}}<div class="source">provider: <code>{{.ProviderAddress}}</code></div>{{end}}{{with diffText .Diff}}<details><summary>Field diff</summary><pre>{{.}}</pre></details>{{end}}</td>
          <td class="source">{{sourceText .Source}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty">No resource changes.</div>
    {{end}}

    <h2>Operations</h2>
    {{if .Operations}}
    <table>
      <thead><tr><th>Action</th><th>Address</th><th>Summary</th><th>Command</th></tr></thead>
      <tbody>
      {{range .Operations}}
        <tr data-plan-row data-action="{{.Action}}" data-host="{{hostText .Address}}" data-search="{{.Address}} {{.Summary}} {{.CommandPreview}}">
          <td><span class="action action-{{.Action}}">{{actionText .Action}}</span></td>
          <td><code>{{.Address}}</code></td>
          <td>{{.Summary}}</td>
          <td><code>{{.CommandPreview}}</code></td>
        </tr>
      {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty">No operations.</div>
    {{end}}
  </main>
  <script>
    const actionFilter = document.getElementById("action-filter");
    const hostFilter = document.getElementById("host-filter");
    const searchFilter = document.getElementById("search-filter");
    const rows = Array.from(document.querySelectorAll("[data-plan-row]"));

    function applyFilters() {
      const action = actionFilter.value;
      const host = hostFilter.value;
      const query = searchFilter.value.trim().toLowerCase();
      for (const row of rows) {
        const matchesAction = !action || row.dataset.action === action;
        const matchesHost = !host || row.dataset.host === host;
        const matchesQuery = !query || row.dataset.search.toLowerCase().includes(query);
        row.classList.toggle("hidden", !(matchesAction && matchesHost && matchesQuery));
      }
    }

    actionFilter.addEventListener("change", applyFilters);
    hostFilter.addEventListener("change", applyFilters);
    searchFilter.addEventListener("input", applyFilters);
  </script>
</body>
</html>
`
