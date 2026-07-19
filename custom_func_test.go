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

// ========== FuncMap — reusable function collection ==========

func TestFuncMap_SharedAcrossValidators(t *testing.T) {
	funcs := NewFuncMap(
		Func("double", func(args ...any) (bloblang.Function, error) {
			n := args[0].(int64)
			return func() (any, error) { return n * 2, nil }, nil
		}),
		Method("is_positive", func(v any) (any, error) {
			n, ok := v.(int64)
			if !ok {
				return false, nil
			}
			return n > 0, nil
		}),
	)
	if funcs.Err() != nil {
		t.Fatalf("FuncMap error: %v", funcs.Err())
	}

	// Same FuncMap used by two different validators
	v1, err := New(`{
		amount: int
		doubled: number @blob(double(this.amount))
	}`, WithFuncMap(funcs))
	if err != nil {
		t.Fatalf("v1: %v", err)
	}

	v2, err := New(`{
		score: int
		check: bool @blob(this.score.is_positive())
	}`, WithFuncMap(funcs))
	if err != nil {
		t.Fatalf("v2: %v", err)
	}

	r := v1.Process(map[string]any{"amount": int64(50)})
	if !r.Valid || r.Output["doubled"] != int64(100) {
		t.Errorf("v1: expected doubled=100, got %v", r.Output["doubled"])
	}

	r = v2.Process(map[string]any{"score": int64(10)})
	if !r.Valid {
		t.Errorf("v2: expected valid, got %v", r.Errors)
	}

	r = v2.Process(map[string]any{"score": int64(-1)})
	if r.Valid {
		t.Error("v2: expected invalid for negative score")
	}
}

func TestFuncMap_OptionStyle(t *testing.T) {
	funcs := NewFuncMap(
		Func("add_one", func(args ...any) (bloblang.Function, error) {
			n := args[0].(int64)
			return func() (any, error) { return n + 1, nil }, nil
		}),
		Method("is_even", func(v any) (any, error) {
			n, ok := v.(int64)
			return ok && n%2 == 0, nil
		}),
	)

	v, err := New(`{
		n:     int
		n1:    number @blob(add_one(this.n))
		check: bool   @blob(this.n.is_even())
	}`, WithFuncMap(funcs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r := v.Process(map[string]any{"n": int64(4)})
	if !r.Valid {
		t.Errorf("expected valid, got %v", r.Errors)
	}
	if r.Output["n1"] != int64(5) {
		t.Errorf("expected n1=5, got %v", r.Output["n1"])
	}
}

func TestFuncMap_CombineWithSingleOptions(t *testing.T) {
	funcs := NewFuncMap(
		Method("triple", func(v any) (any, error) {
			n := v.(int64)
			return n * 3, nil
		}),
	)

	// FuncMap + individual WithFunction + WithErrorFormatter — all work together
	v, err := New(`{
		x:       int
		tripled: number @blob(this.x.triple())
		check:   bool   @blob(is_ok(this.x))
	}`,
		WithFuncMap(funcs),
		WithFunction("is_ok", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r := v.Process(map[string]any{"x": int64(7)})
	if !r.Valid {
		t.Errorf("expected valid, got %v", r.Errors)
	}
	if r.Output["tripled"] != int64(21) {
		t.Errorf("expected tripled=21, got %v", r.Output["tripled"])
	}
}

func TestFuncMap_InvalidName(t *testing.T) {
	funcs := NewFuncMap(
		Func("ValidName", func(args ...any) (bloblang.Function, error) { // uppercase — invalid
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if funcs.Err() == nil {
		t.Fatal("expected error for invalid name 'ValidName'")
	}

	// Using invalid FuncMap in New() should return error
	_, err := New(`{ x: string }`, WithFuncMap(funcs))
	if err == nil {
		t.Fatal("expected New to fail with invalid FuncMap")
	}
}

func TestFuncMap_InvalidNameVariants(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"check_blacklist", true},
		{"is_email", true},
		{"luhn_valid", true},
		{"a1b2", true},
		{"x", true},
		{"CheckBlacklist", false},  // uppercase
		{"check-blacklist", false}, // dash
		{"_leading", false},        // leading underscore
		{"check__double", false},   // double underscore
		{"123start", true},         // digits allowed at start
		{"", false},                // empty
	}

	for _, tt := range tests {
		err := validateName(tt.name)
		if tt.valid && err != nil {
			t.Errorf("validateName(%q) should be valid, got: %v", tt.name, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateName(%q) should be invalid", tt.name)
		}
	}
}

// ========== Conflict Detection ==========

func TestConflict_BuiltinMethodBlocked(t *testing.T) {
	_, err := New(`{ email: string }`,
		WithMethod("is_email", func(v any) (any, error) {
			return false, nil
		}),
	)
	if err == nil {
		t.Fatal("expected error when registering method that conflicts with builtin")
	}
	if !strings.Contains(err.Error(), "conflicts with a built-in") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConflict_BuiltinFunctionBlocked(t *testing.T) {
	_, err := New(`{ d: string }`,
		WithFunction("is_valid_date", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if err == nil {
		t.Fatal("expected error for conflicting function name")
	}
}

func TestConflict_FuncMapBuiltinBlocked(t *testing.T) {
	funcs := NewFuncMap(
		Method("luhn_valid", func(v any) (any, error) { return false, nil }),
	)
	_, err := New(`{ pan: string }`, WithFuncMap(funcs))
	if err == nil {
		t.Fatal("expected error for conflicting FuncMap method")
	}
}

func TestConflict_CrossNamespaceAllowed(t *testing.T) {
	// is_email 是内置 Method，用户注册同名 Function 不冲突（不同命名空间）
	v, err := New(`{ email: string, check: bool @blob(is_email(this.email)) }`,
		WithFunction("is_email", func(args ...any) (bloblang.Function, error) {
			s, _ := args[0].(string)
			return func() (any, error) {
				return strings.Contains(s, "@"), nil
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("cross-namespace should not conflict: %v", err)
	}
	r := v.Process(map[string]any{"email": "a@b.com"})
	if !r.Valid {
		t.Errorf("expected valid: %v", r.Errors)
	}
}

func TestConflict_NonBuiltinNameAllowed(t *testing.T) {
	_, err := New(`{ x: int, check: bool @blob(my_custom_check(this.x)) }`,
		WithFunction("my_custom_check", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if err != nil {
		t.Fatalf("non-conflicting name should work: %v", err)
	}
}

func TestConflict_WithOverrideAllows(t *testing.T) {
	// 显式 WithOverride 允许覆盖 is_email
	v, err := New(`{ email: string, check: bool @blob(this.email.is_email()) }`,
		WithOverrideMethod("is_email"),
		WithMethod("is_email", func(v any) (any, error) {
			// 自定义逻辑：只接受 @company.com 结尾
			s, _ := v.(string)
			return strings.HasSuffix(s, "@company.com"), nil
		}),
	)
	if err != nil {
		t.Fatalf("expected no error with WithOverride: %v", err)
	}

	// 公司邮箱通过
	r := v.Process(map[string]any{"email": "alice@company.com"})
	if !r.Valid {
		t.Error("expected valid for @company.com")
	}

	// 普通邮箱被自定义逻辑拒绝
	r = v.Process(map[string]any{"email": "alice@gmail.com"})
	if r.Valid {
		t.Error("expected invalid — custom is_email rejects non-company emails")
	}
}

func TestConflict_WithOverrideMultiple(t *testing.T) {
	_, err := New(`{ x: string }`,
		WithOverrideMethod("is_email", "luhn_valid"),
		WithMethod("is_email", func(v any) (any, error) { return true, nil }),
		WithMethod("luhn_valid", func(v any) (any, error) { return true, nil }),
	)
	if err != nil {
		t.Fatalf("expected no error with multiple overrides: %v", err)
	}
}

func TestConflict_WithOverrideOnlySpecified(t *testing.T) {
	// Override is_email but NOT luhn_valid — luhn_valid should still be blocked
	_, err := New(`{ x: string }`,
		WithOverrideMethod("is_email"),
		WithMethod("is_email", func(v any) (any, error) { return true, nil }),
		WithMethod("luhn_valid", func(v any) (any, error) { return true, nil }),
	)
	if err == nil {
		t.Fatal("expected error — luhn_valid not in override list")
	}
	if !strings.Contains(err.Error(), "luhn_valid") {
		t.Errorf("error should mention luhn_valid: %v", err)
	}
}

func TestConflict_WithOverrideAll(t *testing.T) {
	// WithOverrideAll 允许覆盖任何内置
	v, err := New(`{
		email: string
		check: bool @blob(this.email.is_email())
		date:  string
		valid: bool @blob(is_valid_date(this.date))
	}`,
		WithOverrideAll(),
		WithMethod("is_email", func(v any) (any, error) { return true, nil }),
		WithFunction("is_valid_date", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if err != nil {
		t.Fatalf("WithOverrideAll should allow everything: %v", err)
	}
	r := v.Process(map[string]any{"email": "anything", "date": "anything"})
	if !r.Valid {
		t.Errorf("all overridden to return true: %v", r.Errors)
	}
}

func TestConflict_WithOverrideFunc(t *testing.T) {
	// 只允许覆盖 function，method 仍然受保护
	_, err := New(`{ d: string }`,
		WithOverrideFunc("is_valid_date"),
		WithFunction("is_valid_date", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if err != nil {
		t.Fatalf("WithOverrideFunc should allow: %v", err)
	}

	// 但 method 仍然受保护
	_, err = New(`{ x: string }`,
		WithOverrideFunc("is_email"), // 这个对 method 无效
		WithMethod("is_email", func(v any) (any, error) { return true, nil }),
	)
	if err == nil {
		t.Fatal("WithOverrideFunc should NOT protect methods")
	}
}
