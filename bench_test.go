package schemix

import (
	"testing"
)

// ---------- Core Validator Benchmarks ----------

var benchSchema = `{
	pan:      =~"^[0-9]{16}$"
	amount:   int & >0
	currency: "156" | "840"

	pan_check:  bool   @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))
	card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
	fee:        number @blob(if this.currency == "156" { 0 } else { (this.amount * 0.015).ceil() })
}`

var benchDataValid = map[string]any{
	"pan": "6222021234567890", "amount": int64(10000), "currency": "156",
}

var benchDataInvalid = map[string]any{
	"pan": "ABC", "amount": int64(-1), "currency": "999",
}

func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		_, _ = New(benchSchema)
	}
}

func BenchmarkProcess_Valid(b *testing.B) {
	v := MustNew(benchSchema)
	b.ResetTimer()
	for b.Loop() {
		v.Process(benchDataValid)
	}
}

func BenchmarkProcess_Invalid(b *testing.B) {
	v := MustNew(benchSchema)
	b.ResetTimer()
	for b.Loop() {
		v.Process(benchDataInvalid)
	}
}

func BenchmarkValidate_Valid(b *testing.B) {
	v := MustNew(benchSchema)
	b.ResetTimer()
	for b.Loop() {
		v.Validate(benchDataValid)
	}
}

func BenchmarkProcessWithMode_FailFast(b *testing.B) {
	v := MustNew(benchSchema)
	b.ResetTimer()
	for b.Loop() {
		v.ProcessWithMode(benchDataInvalid, FailFast)
	}
}

func BenchmarkProcessWithMode_FailAll(b *testing.B) {
	v := MustNew(benchSchema)
	b.ResetTimer()
	for b.Loop() {
		v.ProcessWithMode(benchDataInvalid, FailAll)
	}
}

// ---------- Nested Schema Benchmark ----------

var benchNestedSchema = `{
	order_id: =~"^ORD-[0-9]+$"
	customer: {
		name:  string
		email: =~"^.+@.+\\..+$"
	}
	items: [...{
		product: string
		price:   number & >0
		qty:     int & >=1
	}]
	total: number @blob(this.items.map_each(this.price * this.qty).sum())
}`

var benchNestedData = map[string]any{
	"order_id": "ORD-12345",
	"customer": map[string]any{"name": "Alice", "email": "alice@test.com"},
	"items": []any{
		map[string]any{"product": "Laptop", "price": 5999.0, "qty": int64(1)},
		map[string]any{"product": "Mouse", "price": 99.0, "qty": int64(2)},
		map[string]any{"product": "Keyboard", "price": 299.0, "qty": int64(1)},
	},
}

func BenchmarkProcess_Nested(b *testing.B) {
	v := MustNew(benchNestedSchema)
	b.ResetTimer()
	for b.Loop() {
		v.Process(benchNestedData)
	}
}

// ---------- Meta Features Benchmark ----------

var benchMetaSchema = `{
	mti:       =~"^[01][0-9]{3}$" @meta(priority=1,fail_fast)
	pan:       =~"^[0-9]{13,19}$" @meta(priority=1,skip_empty)
	amount:    int & >0            @meta(priority=2)
	currency:  "156" | "840"       @meta(priority=1)
	auth_code?: =~"^[A-Z0-9]{6}$" @meta(optional,conditional,required_if=this.mti == "0110")
	memo?:     string              @meta(optional,omit_empty)
}`

var benchMetaData = map[string]any{
	"mti": "0100", "pan": "6222021234567890", "amount": int64(10000), "currency": "156", "memo": "",
}

func BenchmarkProcess_Meta(b *testing.B) {
	v := MustNew(benchMetaSchema)
	b.ResetTimer()
	for b.Loop() {
		v.Process(benchMetaData)
	}
}

func BenchmarkProcessWithMode_FailPriority(b *testing.B) {
	v := MustNew(benchMetaSchema)
	bad := map[string]any{
		"mti": "9999", "pan": "ABC", "amount": int64(-1), "currency": "999",
	}
	b.ResetTimer()
	for b.Loop() {
		v.ProcessWithMode(bad, FailPriority)
	}
}

// ---------- deepCopy Benchmark ----------

func BenchmarkDeepCopy_Small(b *testing.B) {
	data := map[string]any{"pan": "6222021234567890", "amount": int64(10000), "currency": "156"}
	b.ResetTimer()
	for b.Loop() {
		deepCopy(data)
	}
}

func BenchmarkDeepCopy_Nested(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		deepCopy(benchNestedData)
	}
}

// ---------- Registry Benchmark ----------

func BenchmarkRegistry_Get(b *testing.B) {
	reg := NewRegistry()
	_ = reg.Register("payment", benchSchema)
	b.ResetTimer()
	for b.Loop() {
		reg.Get("payment")
	}
}

func BenchmarkRegistry_Register(b *testing.B) {
	for b.Loop() {
		reg := NewRegistry()
		_ = reg.Register("payment", benchSchema)
	}
}

// ---------- Parallel Benchmarks ----------

func BenchmarkProcess_Parallel(b *testing.B) {
	v := MustNew(benchSchema)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			v.Process(benchDataValid)
		}
	})
}

func BenchmarkRegistry_Get_Parallel(b *testing.B) {
	reg := NewRegistry()
	_ = reg.Register("payment", benchSchema)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reg.Get("payment")
		}
	})
}

// ---------- CUE Layer Breakdown ----------

func BenchmarkCUE_Encode(b *testing.B) {
	v := MustNew(benchSchema)
	b.ResetTimer()
	for b.Loop() {
		v.ctx.Encode(benchDataValid)
	}
}

func BenchmarkCUE_ValidateFields(b *testing.B) {
	v := MustNew(benchSchema)
	dataValue := v.ctx.Encode(benchDataValid)
	b.ResetTimer()
	for b.Loop() {
		result := &Result{Valid: true, Errors: []ValidationError{}}
		v.validateCUEFields(v.cueFields, dataValue, benchDataValid, result)
	}
}

func BenchmarkCUE_ValidateRecursive_Legacy(b *testing.B) {
	v := MustNew(benchSchema)
	dataValue := v.ctx.Encode(benchDataValid)
	b.ResetTimer()
	for b.Loop() {
		result := &Result{Valid: true, Errors: []ValidationError{}}
		v.validateCUERecursive(v.schema, dataValue, "", result)
	}
}

func BenchmarkBlob_RulesOnly(b *testing.B) {
	v := MustNew(benchSchema)
	b.ResetTimer()
	for b.Loop() {
		for _, rule := range v.blobRules {
			if rule.Exec != nil {
				_, _ = rule.Exec.Query(benchDataValid)
			}
		}
	}
}
