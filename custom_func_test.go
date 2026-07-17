package schemix

import (
	"fmt"
	"strings"
	"testing"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

// ========== WithFunction (FunctionConstructor style) ==========

func TestWithFunction_SimpleValidation(t *testing.T) {
	isEven := func(args ...any) (bloblang.Function, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("is_even requires 1 argument")
		}
		n := args[0]
		return func() (any, error) {
			switch v := n.(type) {
			case int64:
				return v%2 == 0, nil
			default:
				return nil, fmt.Errorf("is_even requires int64, got %T", n)
			}
		}, nil
	}

	v, err := New(`{
		amount: int & >0
		even_check: bool @blob(is_even(this.amount))
	}`, WithFunction("is_even", isEven))
	if err != nil {
		t.Fatalf("New with custom function: %v", err)
	}

	r := v.Process(map[string]any{"amount": int64(100)})
	if !r.Valid {
		t.Errorf("expected valid for even amount, got errors: %v", r.Errors)
	}

	r = v.Process(map[string]any{"amount": int64(99)})
	if r.Valid {
		t.Error("expected invalid for odd amount")
	}
}

func TestWithFunction_ComputedValue(t *testing.T) {
	toUpper := func(args ...any) (bloblang.Function, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("to_upper requires 1 argument")
		}
		s, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("to_upper requires string, got %T", args[0])
		}
		return func() (any, error) {
			return strings.ToUpper(s), nil
		}, nil
	}

	v, err := New(`{
		name:  string
		upper: string @blob(to_upper(this.name))
	}`, WithFunction("to_upper", toUpper))
	if err != nil {
		t.Fatalf("New with custom function: %v", err)
	}

	r := v.Process(map[string]any{"name": "alice"})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["upper"] != "ALICE" {
		t.Errorf("expected upper='ALICE', got %v", r.Output["upper"])
	}
}

func TestWithFunction_MultipleCustomFunctions(t *testing.T) {
	isPositive := func(args ...any) (bloblang.Function, error) {
		v, ok := args[0].(int64)
		if !ok {
			return nil, fmt.Errorf("requires int64")
		}
		return func() (any, error) { return v > 0, nil }, nil
	}

	double := func(args ...any) (bloblang.Function, error) {
		v, ok := args[0].(int64)
		if !ok {
			return nil, fmt.Errorf("requires int64")
		}
		return func() (any, error) { return v * 2, nil }, nil
	}

	v, err := New(`{
		amount:      int
		pos_check:   bool   @blob(is_positive(this.amount))
		doubled:     number @blob(double(this.amount))
	}`, WithFunction("is_positive", isPositive), WithFunction("double", double))
	if err != nil {
		t.Fatalf("New with multiple custom functions: %v", err)
	}

	r := v.Process(map[string]any{"amount": int64(50)})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["doubled"] != int64(100) {
		t.Errorf("expected doubled=100, got %v (%T)", r.Output["doubled"], r.Output["doubled"])
	}
}

func TestWithFunction_IsolationBetweenValidators(t *testing.T) {
	myFunc := func(args ...any) (bloblang.Function, error) {
		return func() (any, error) { return true, nil }, nil
	}

	_, err := New(`{
		x: bool @blob(my_custom_fn())
	}`, WithFunction("my_custom_fn", myFunc))
	if err != nil {
		t.Fatalf("v1 creation failed: %v", err)
	}

	// v2 does NOT have the custom function — should fail to compile
	_, err = New(`{
		x: bool @blob(my_custom_fn())
	}`)
	if err == nil {
		t.Fatal("expected compilation error for unregistered function in v2")
	}
}

func TestWithFunction_NoCustomFunctions(t *testing.T) {
	v := MustNew(`{
		name: string
		check: bool @blob(this.name.length() > 0)
	}`)

	r := v.Process(map[string]any{"name": "hello"})
	if !r.Valid {
		t.Errorf("expected valid without custom functions, got errors: %v", r.Errors)
	}
}

func TestWithFunction_ErrorInCustomFunction(t *testing.T) {
	failing := func(args ...any) (bloblang.Function, error) {
		return func() (any, error) {
			return nil, fmt.Errorf("external service unavailable")
		}, nil
	}

	v, err := New(`{
		pan:   string
		check: bool @blob(check_blacklist(this.pan))
	}`, WithFunction("check_blacklist", failing))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	r := v.Process(map[string]any{"pan": "1234567890123456"})
	if r.Valid {
		t.Fatal("expected invalid when custom function returns error")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeExprExecError && e.Path == "check" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected E2X01 for 'check', got: %v", r.Errors)
	}
}

func TestWithFunction_WithRegistry(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register("basic", `{ name: string }`)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	v, ok := reg.Get("basic")
	if !ok {
		t.Fatal("expected to get basic validator")
	}
	r := v.Process(map[string]any{"name": "test"})
	if !r.Valid {
		t.Errorf("expected valid, got: %v", r.Errors)
	}
}

// ========== WithFunctionV2 (PluginSpec + ParsedParams) ==========

func TestWithFunctionV2_TypedParams(t *testing.T) {
	v, err := New(`{
		amount: int & >0
		fee:    number @blob(calculate_fee(this.amount, 0.015))
	}`, WithFunctionV2("calculate_fee",
		bloblang.NewPluginSpec().
			Param(bloblang.NewInt64Param("amount")).
			Param(bloblang.NewFloat64Param("rate")),
		func(args *bloblang.ParsedParams) (bloblang.Function, error) {
			amount, err := args.GetInt64("amount")
			if err != nil {
				return nil, err
			}
			rate, err := args.GetFloat64("rate")
			if err != nil {
				return nil, err
			}
			return func() (any, error) {
				return float64(amount) * rate, nil
			}, nil
		},
	))
	if err != nil {
		t.Fatalf("New with WithFunctionV2: %v", err)
	}

	r := v.Process(map[string]any{"amount": int64(10000)})
	if !r.Valid {
		t.Errorf("expected valid, got: %v", r.Errors)
	}
	if fee, ok := r.Output["fee"].(float64); !ok || fee != 150.0 {
		t.Errorf("expected fee=150.0, got %v (%T)", r.Output["fee"], r.Output["fee"])
	}
}

// ========== WithMethod (called on target value) ==========

func TestWithMethod_SimpleMethod(t *testing.T) {
	isUnionpay := func(v any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return false, nil
		}
		return strings.HasPrefix(s, "62"), nil
	}

	v, err := New(`{
		pan:   =~"^[0-9]{16}$"
		check: bool @blob(this.pan.is_unionpay())
	}`, WithMethod("is_unionpay", isUnionpay))
	if err != nil {
		t.Fatalf("New with WithMethod: %v", err)
	}

	r := v.Process(map[string]any{"pan": "6222021234567890"})
	if !r.Valid {
		t.Errorf("expected valid for UnionPay PAN, got: %v", r.Errors)
	}

	r = v.Process(map[string]any{"pan": "4111111111111111"})
	if r.Valid {
		t.Error("expected invalid for non-UnionPay PAN")
	}
}

// ========== WithMethodV2 (PluginSpec + ParsedParams) ==========

func TestWithMethodV2_WithParams(t *testing.T) {
	v, err := New(`{
		name:  string
		check: bool @blob(this.name.min_length(length: 3))
	}`, WithMethodV2("min_length",
		bloblang.NewPluginSpec().
			Param(bloblang.NewInt64Param("length")),
		func(args *bloblang.ParsedParams) (bloblang.Method, error) {
			minLen, err := args.GetInt64("length")
			if err != nil {
				return nil, err
			}
			return func(v any) (any, error) {
				s, ok := v.(string)
				if !ok {
					return false, nil
				}
				return int64(len(s)) >= minLen, nil
			}, nil
		},
	))
	if err != nil {
		t.Fatalf("New with WithMethodV2: %v", err)
	}

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Errorf("expected valid for 'Alice', got: %v", r.Errors)
	}

	r = v.Process(map[string]any{"name": "AB"})
	if r.Valid {
		t.Error("expected invalid for 'AB' (length < 3)")
	}
}

// ========== Mixed ==========

func TestWithMethod_MixedFunctionsAndMethods(t *testing.T) {
	computeTax := func(args ...any) (bloblang.Function, error) {
		amount, ok := args[0].(int64)
		if !ok {
			return nil, fmt.Errorf("requires int64")
		}
		return func() (any, error) {
			return float64(amount) * 0.1, nil
		}, nil
	}

	toUpper := func(v any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return v, nil
		}
		return strings.ToUpper(s), nil
	}

	v, err := New(`{
		name:   string
		amount: int & >0
		upper:  string @blob(this.name.to_upper())
		tax:    number @blob(compute_tax(this.amount))
	}`,
		WithFunction("compute_tax", computeTax),
		WithMethod("to_upper", toUpper),
	)
	if err != nil {
		t.Fatalf("New with mixed: %v", err)
	}

	r := v.Process(map[string]any{"name": "alice", "amount": int64(1000)})
	if !r.Valid {
		t.Errorf("expected valid, got: %v", r.Errors)
	}
	if r.Output["upper"] != "ALICE" {
		t.Errorf("expected upper=ALICE, got %v", r.Output["upper"])
	}
	if tax, ok := r.Output["tax"].(float64); !ok || tax != 100.0 {
		t.Errorf("expected tax=100.0, got %v", r.Output["tax"])
	}
}
