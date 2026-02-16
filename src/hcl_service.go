package main

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

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

		out[k] = ctyToGo(v)
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


func ctyToGo(v cty.Value) any {
	if !v.IsKnown() || v.IsNull() {
		return nil
	}

	switch v.Type() {
	case cty.String:
		return v.AsString()

	case cty.Number:
		i, _ := v.AsBigFloat().Int64()
		return i

	case cty.Bool:
		return v.True()

	default:
		if v.Type().IsTupleType() || v.Type().IsListType() {
			var out []any
			for _, ev := range v.AsValueSlice() {
				out = append(out, ctyToGo(ev))
			}
			return out
		}

		if v.Type().IsObjectType() || v.Type().IsMapType() {
			out := map[string]any{}
			for k, ev := range v.AsValueMap() {
				out[k] = ctyToGo(ev)
			}
			return out
		}
	}

	return nil
}
