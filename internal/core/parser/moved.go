package parser

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mofelee/debianform/internal/core/ir"
)

type Moved struct {
	From       string
	To         string
	Source     ir.SourceRef
	FromSource ir.SourceRef
	ToSource   ir.SourceRef
}

func parseMovedBlock(file string, block *hclsyntax.Block) (Moved, error) {
	if len(block.Labels) != 0 {
		return Moved{}, fmt.Errorf("%s:%d: moved block must not have labels", file, block.TypeRange.Start.Line)
	}
	if len(block.Body.Blocks) != 0 {
		return Moved{}, fmt.Errorf("%s:%d: moved block does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line)
	}
	unsupported := make([]string, 0)
	for name := range block.Body.Attributes {
		if name != "from" && name != "to" {
			unsupported = append(unsupported, name)
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		name := unsupported[0]
		attr := block.Body.Attributes[name]
		return Moved{}, fmt.Errorf("%s:%d: unsupported attribute moved.%s", file, attr.NameRange.Start.Line, name)
	}
	fromAttr, ok := block.Body.Attributes["from"]
	if !ok {
		return Moved{}, fmt.Errorf("%s:%d: moved.from is required", file, block.TypeRange.Start.Line)
	}
	toAttr, ok := block.Body.Attributes["to"]
	if !ok {
		return Moved{}, fmt.Errorf("%s:%d: moved.to is required", file, block.TypeRange.Start.Line)
	}
	from, err := parseMovedComponentTraversal(file, "from", fromAttr.Expr)
	if err != nil {
		return Moved{}, err
	}
	to, err := parseMovedComponentTraversal(file, "to", toAttr.Expr)
	if err != nil {
		return Moved{}, err
	}
	path := fmt.Sprintf("moved[component.%s]", from)
	return Moved{
		From:       from,
		To:         to,
		Source:     ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path},
		FromSource: ir.SourceRef{File: file, Line: fromAttr.NameRange.Start.Line, Path: path + ".from"},
		ToSource:   ir.SourceRef{File: file, Line: toAttr.NameRange.Start.Line, Path: path + ".to"},
	}, nil
}

func parseMovedComponentTraversal(file, endpoint string, expr hcl.Expression) (string, error) {
	traversal, diags := hcl.AbsTraversalForExpr(expr)
	if diags.HasErrors() || len(traversal) != 2 {
		return "", fmt.Errorf("%s:%d: moved.%s must be a static component.<name> traversal", file, expr.Range().Start.Line, endpoint)
	}
	root, rootOK := traversal[0].(hcl.TraverseRoot)
	name, nameOK := traversal[1].(hcl.TraverseAttr)
	if !rootOK || root.Name != "component" || !nameOK || name.Name == "" {
		return "", fmt.Errorf("%s:%d: moved.%s must be a static component.<name> traversal", file, expr.Range().Start.Line, endpoint)
	}
	return name.Name, nil
}

func validateAndSortMoved(cfg *Config) error {
	moves := append([]Moved(nil), cfg.Moves...)
	sort.SliceStable(moves, func(i, j int) bool {
		if moves[i].Source.File != moves[j].Source.File {
			return moves[i].Source.File < moves[j].Source.File
		}
		if moves[i].Source.Line != moves[j].Source.Line {
			return moves[i].Source.Line < moves[j].Source.Line
		}
		if moves[i].From != moves[j].From {
			return moves[i].From < moves[j].From
		}
		return moves[i].To < moves[j].To
	})

	bySource := make(map[string]Moved, len(moves))
	byTarget := make(map[string]Moved, len(moves))
	for _, move := range moves {
		if move.From == move.To {
			return movedError(move.FromSource, "component.%s cannot move to itself", move.From)
		}
		if previous, ok := bySource[move.From]; ok {
			if previous.To == move.To {
				return movedError(move.FromSource, "duplicate mapping from component.%s to component.%s; first defined at %s:%d", move.From, move.To, previous.Source.File, previous.Source.Line)
			}
			return movedError(move.FromSource, "component.%s maps to both component.%s and component.%s; first defined at %s:%d", move.From, previous.To, move.To, previous.Source.File, previous.Source.Line)
		}
		if previous, ok := byTarget[move.To]; ok && previous.From != move.From {
			return movedError(move.FromSource, "component.%s and component.%s both map to component.%s; first defined at %s:%d", previous.From, move.From, move.To, previous.Source.File, previous.Source.Line)
		}
		bySource[move.From] = move
		byTarget[move.To] = move
	}

	state := map[string]uint8{}
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			move := bySource[name]
			return movedError(move.FromSource, "moved mappings contain a cycle through component.%s", name)
		case 2:
			return nil
		}
		state[name] = 1
		if move, ok := bySource[name]; ok {
			if err := visit(move.To); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	sources := make([]string, 0, len(bySource))
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		if err := visit(source); err != nil {
			return err
		}
	}

	sort.SliceStable(moves, func(i, j int) bool {
		if moves[i].From != moves[j].From {
			return moves[i].From < moves[j].From
		}
		if moves[i].To != moves[j].To {
			return moves[i].To < moves[j].To
		}
		if moves[i].Source.File != moves[j].Source.File {
			return moves[i].Source.File < moves[j].Source.File
		}
		return moves[i].Source.Line < moves[j].Source.Line
	})
	cfg.Moves = moves
	return nil
}

func movedError(source ir.SourceRef, format string, args ...any) error {
	return fmt.Errorf("%s:%d:%s: %s", source.File, source.Line, source.Path, fmt.Sprintf(format, args...))
}
