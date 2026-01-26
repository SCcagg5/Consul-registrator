package main

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

/// ParseServiceHCL parses a Docker label containing a single Consul service block.
func ParseServiceHCL(input string) (map[string]any, error) {
	parser := hclparse.NewParser()
	f, diags := parser.ParseHCL([]byte(input), "label.hcl")
	if diags.HasErrors() {
		return nil, fmt.Errorf(diags.Error())
	}

	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("invalid HCL body")
	}

	var svc *hclsyntax.Block
	for _, b := range body.Blocks {
		if b.Type == "service" {
			if svc != nil {
				return nil, fmt.Errorf("multiple service blocks")
			}
			svc = b
		}
	}

	if svc == nil {
		return nil, fmt.Errorf("missing service block")
	}

	return hclBodyToMap(svc.Body)
}

func hclBodyToMap(body *hclsyntax.Body) (map[string]any, error) {
	out := map[string]any{}

	for k, a := range body.Attributes {
		v, diags := a.Expr.Value(&hcl.EvalContext{})
		if diags.HasErrors() {
			return nil, fmt.Errorf(diags.Error())
		}
		out[k] = v.AsString()
	}

	for _, b := range body.Blocks {
		child, err := hclBodyToMap(b.Body)
		if err != nil {
			return nil, err
		}
		out[b.Type] = child
	}

	return out, nil
}
