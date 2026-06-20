package config

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Block struct {
	Type  string
	Label string
	Attrs map[string]Expr
	File  string
	Line  int
}

type Expr = hcl.Expression

type Number string

func ParseFile(file string) ([]Block, error) {
	parser := hclparse.NewParser()
	parsed, diags := parser.ParseHCLFile(file)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}

	body, ok := parsed.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("%s: unsupported HCL body type %T", file, parsed.Body)
	}

	blocks := make([]Block, 0, len(body.Blocks))
	for _, block := range body.Blocks {
		line := block.TypeRange.Start.Line
		if len(block.Labels) > 1 {
			return nil, fmt.Errorf("%s:%d: block %s supports at most one label", file, line, block.Type)
		}

		label := ""
		if len(block.Labels) == 1 {
			label = block.Labels[0]
		}

		attrs := make(map[string]Expr, len(block.Body.Attributes))
		for name, attr := range block.Body.Attributes {
			attrs[name] = attr.Expr
		}

		blocks = append(blocks, Block{
			Type:  block.Type,
			Label: label,
			Attrs: attrs,
			File:  file,
			Line:  line,
		})
	}

	return blocks, nil
}
