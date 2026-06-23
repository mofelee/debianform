package parser

import (
	"encoding/json"
	"fmt"

	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/zclconf/go-cty/cty"
)

type Modifier string

const (
	ModifierNone   Modifier = ""
	ModifierForce  Modifier = "force"
	ModifierBefore Modifier = "before"
	ModifierAfter  Modifier = "after"
	ModifierUnset  Modifier = "unset"
)

type Kind string

const (
	KindNull   Kind = "null"
	KindString Kind = "string"
	KindBool   Kind = "bool"
	KindNumber Kind = "number"
	KindList   Kind = "list"
	KindMap    Kind = "map"
)

type Value struct {
	Kind      Kind
	String    string
	Bool      bool
	Number    string
	List      []Value
	Map       map[string]Value
	Source    ir.SourceRef
	Modifier  Modifier
	Sensitive bool
	Ephemeral bool
}

const SensitiveMark = "debianform:sensitive"
const EphemeralMark = "debianform:ephemeral"

func NullValue(source ir.SourceRef) Value {
	return Value{Kind: KindNull, Source: source}
}

func MapValue(values map[string]Value, source ir.SourceRef) Value {
	if values == nil {
		values = map[string]Value{}
	}
	return Value{Kind: KindMap, Map: values, Source: source}
}

func (v Value) WithoutModifier() Value {
	v.Modifier = ModifierNone
	return v
}

func (v Value) IsMap() bool {
	return v.Kind == KindMap
}

func (v Value) IsList() bool {
	return v.Kind == KindList
}

func (v Value) ContainsSensitive() bool {
	if v.Sensitive {
		return true
	}
	switch v.Kind {
	case KindList:
		for _, item := range v.List {
			if item.ContainsSensitive() {
				return true
			}
		}
	case KindMap:
		for _, item := range v.Map {
			if item.ContainsSensitive() {
				return true
			}
		}
	}
	return false
}

func (v Value) ContainsEphemeral() bool {
	if v.Ephemeral {
		return true
	}
	switch v.Kind {
	case KindList:
		for _, item := range v.List {
			if item.ContainsEphemeral() {
				return true
			}
		}
	case KindMap:
		for _, item := range v.Map {
			if item.ContainsEphemeral() {
				return true
			}
		}
	}
	return false
}

func (v Value) StringValue() (string, bool) {
	switch v.Kind {
	case KindString:
		return v.String, true
	case KindNumber:
		return v.Number, true
	default:
		return "", false
	}
}

func (v Value) Key() string {
	data, err := json.Marshal(v.canonical())
	if err != nil {
		return fmt.Sprintf("%s:%s", v.Kind, v.String)
	}
	return string(data)
}

func (v Value) CanonicalString() string {
	data, err := json.Marshal(v.canonicalTypeLiteral())
	if err != nil {
		return "null"
	}
	return string(data)
}

func (v Value) canonicalTypeLiteral() any {
	switch v.Kind {
	case KindNumber:
		return json.Number(v.Number)
	case KindList:
		out := make([]any, 0, len(v.List))
		for _, item := range v.List {
			out = append(out, item.canonicalTypeLiteral())
		}
		return out
	case KindMap:
		out := make(map[string]any, len(v.Map))
		for key, item := range v.Map {
			out[key] = item.canonicalTypeLiteral()
		}
		return out
	default:
		return v.canonical()
	}
}

func (v Value) canonical() any {
	switch v.Kind {
	case KindNull:
		return nil
	case KindString:
		return v.String
	case KindBool:
		return v.Bool
	case KindNumber:
		return v.Number
	case KindList:
		out := make([]any, 0, len(v.List))
		for _, item := range v.List {
			out = append(out, item.canonical())
		}
		return out
	case KindMap:
		out := make(map[string]any, len(v.Map))
		for key, item := range v.Map {
			out[key] = item.canonical()
		}
		return out
	default:
		return nil
	}
}

func (v Value) ToCty() (cty.Value, error) {
	converted, err := v.toCty()
	if err != nil {
		return cty.NilVal, err
	}
	if v.Sensitive {
		converted = converted.Mark(SensitiveMark)
	}
	if v.Ephemeral {
		converted = converted.Mark(EphemeralMark)
	}
	return converted, nil
}

func (v Value) toCty() (cty.Value, error) {
	switch v.Kind {
	case KindNull:
		return cty.NullVal(cty.DynamicPseudoType), nil
	case KindString:
		return cty.StringVal(v.String), nil
	case KindBool:
		return cty.BoolVal(v.Bool), nil
	case KindNumber:
		return cty.ParseNumberVal(v.Number)
	case KindList:
		if len(v.List) == 0 {
			return cty.EmptyTupleVal, nil
		}
		values := make([]cty.Value, 0, len(v.List))
		for _, item := range v.List {
			converted, err := item.ToCty()
			if err != nil {
				return cty.NilVal, err
			}
			values = append(values, converted)
		}
		return cty.TupleVal(values), nil
	case KindMap:
		if len(v.Map) == 0 {
			return cty.EmptyObjectVal, nil
		}
		values := make(map[string]cty.Value, len(v.Map))
		for key, item := range v.Map {
			converted, err := item.ToCty()
			if err != nil {
				return cty.NilVal, err
			}
			values[key] = converted
		}
		return cty.ObjectVal(values), nil
	default:
		return cty.NilVal, fmt.Errorf("unsupported value kind %q", v.Kind)
	}
}
