package plan

import (
	"encoding/json"
	"fmt"
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
	Diff            DiffNode     `json:"diff"`
	LowLevelActions []string     `json:"low_level_actions,omitempty"`
}

type DiffNode struct {
	Path      []string       `json:"path"`
	Kind      string         `json:"kind"`
	Action    string         `json:"action"`
	Sensitive bool           `json:"sensitive"`
	Before    any            `json:"before"`
	After     map[string]any `json:"after,omitempty"`
	Children  []DiffNode     `json:"children,omitempty"`
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
		changes = append(changes, Change{
			Address: node.Address,
			Action:  "create",
			Summary: node.Summary,
			Source:  node.Source,
			Diff: DiffNode{
				Path:      []string{},
				Kind:      "object",
				Action:    "create",
				Sensitive: false,
				Before:    nil,
				After:     node.Desired,
			},
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
			Operations: 0,
		},
		Changes:     changes,
		Operations:  []OperationNode{},
		Diagnostics: []Diagnostic{},
	}
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
		fmt.Fprintf(w, "  + %s\n", change.Address)
		if change.Summary != "" {
			fmt.Fprintf(w, "    %s\n", change.Summary)
		}
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

func printSummary(w io.Writer, summary Summary) {
	fmt.Fprintf(w, "Summary: %d create, %d update, %d delete, %d no-op, %d operations\n",
		summary.Create,
		summary.Update,
		summary.Delete,
		summary.NoOp,
		summary.Operations,
	)
}
