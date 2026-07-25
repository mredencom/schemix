package schemix

import (
	"testing"
)

// TestBuiltinTable exercises every built-in validator method with ≥3 named subtests
// covering valid, invalid, and edge cases (R16 compliance).
func TestBuiltinTable(t *testing.T) {
	type sub struct {
		name  string
		input any
		valid bool
	}

	// Helper: build a validator for a given method expression
	run := func(t *testing.T, schema string, fieldName string, cases []sub) {
		t.Helper()
		v := MustNew(schema)
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				r := v.Process(map[string]any{fieldName: tc.input})
				if r.Valid != tc.valid {
					t.Errorf("%s=%v: got valid=%v, want %v; errors=%v",
						fieldName, tc.input, r.Valid, tc.valid, r.Errors)
				}
			})
		}
	}

	// ===== String Format Methods =====

	t.Run("is_email", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_email()) }`, "v", []sub{
			{"valid/simple", "user@example.com", true},
			{"valid/plus-tag", "user+tag@domain.co", true},
			{"valid/subdomain", "a@sub.domain.org", true},
			{"invalid/no-at", "invalid", false},
			{"invalid/no-domain", "user@", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_url", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_url()) }`, "v", []sub{
			{"valid/https", "https://example.com", true},
			{"valid/http-path", "http://localhost:8080/path", true},
			{"valid/ftp", "ftp://files.example.com/doc.pdf", true},
			{"invalid/no-scheme", "example.com", false},
			{"invalid/spaces", "not a url", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_full_url", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_full_url()) }`, "v", []sub{
			{"valid/https", "https://example.com/path", true},
			{"valid/http", "http://localhost", true},
			{"invalid/ftp", "ftp://files.com", false},
			{"invalid/no-scheme", "example.com", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_uuid", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_uuid()) }`, "v", []sub{
			{"valid/v4", "550e8400-e29b-41d4-a716-446655440000", true},
			{"valid/uppercase", "550E8400-E29B-41D4-A716-446655440000", true},
			{"invalid/no-dashes", "550e8400e29b41d4a716446655440000", false},
			{"invalid/text", "not-a-uuid", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_uuid3", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_uuid3()) }`, "v", []sub{
			{"valid/v3", "f47ac10b-58cc-3a72-a567-0e02b2c3d479", true},
			{"invalid/v4", "f47ac10b-58cc-4a72-a567-0e02b2c3d479", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_uuid4", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_uuid4()) }`, "v", []sub{
			{"valid/v4", "f47ac10b-58cc-4372-a567-0e02b2c3d479", true},
			{"invalid/v1", "550e8400-e29b-11d4-a716-446655440000", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_uuid5", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_uuid5()) }`, "v", []sub{
			{"valid/v5", "f47ac10b-58cc-5a72-a567-0e02b2c3d479", true},
			{"invalid/v4", "f47ac10b-58cc-4a72-a567-0e02b2c3d479", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_ip", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_ip()) }`, "v", []sub{
			{"valid/v4", "192.168.1.1", true},
			{"valid/v6", "::1", true},
			{"valid/v6-full", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", true},
			{"invalid/text", "not-an-ip", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_ipv4", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_ipv4()) }`, "v", []sub{
			{"valid/normal", "10.0.0.1", true},
			{"valid/max", "255.255.255.255", true},
			{"invalid/v6", "::1", false},
			{"invalid/out-range", "256.1.1.1", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_ipv6", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_ipv6()) }`, "v", []sub{
			{"valid/loopback", "::1", true},
			{"valid/full", "fe80::1", true},
			{"invalid/v4", "192.168.1.1", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_cidr", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_cidr()) }`, "v", []sub{
			{"valid/v4", "192.168.1.0/24", true},
			{"valid/v6", "2001:db8::/32", true},
			{"invalid/no-prefix", "192.168.1.1", false},
			{"invalid/bad-prefix", "192.168.1.0/33", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_mac", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_mac()) }`, "v", []sub{
			{"valid/colon", "01:23:45:67:89:ab", true},
			{"valid/dash", "01-23-45-67-89-AB", true},
			{"invalid/short", "01:23:45", false},
			{"invalid/text", "not-a-mac", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_json", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_json()) }`, "v", []sub{
			{"valid/object", `{"key":"value"}`, true},
			{"valid/array", `[1,2,3]`, true},
			{"valid/null", `null`, true},
			{"invalid/broken", `{invalid}`, false},
			{"edge/empty", ``, false},
		})
	})

	t.Run("is_base64", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_base64()) }`, "v", []sub{
			{"valid/hello", "SGVsbG8=", true},
			{"valid/padded", "YWJj", true},
			{"invalid/special-chars", "not!base64", false},
			{"edge/empty", "", false}, // empty string fails base64 check
		})
	})

	t.Run("is_ascii", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_ascii()) }`, "v", []sub{
			{"valid/letters", "hello", true},
			{"valid/digits", "12345", true},
			{"invalid/unicode", "héllo", false},
			{"invalid/emoji", "hi 👋", false},
			{"edge/empty", "", true},
		})
	})

	t.Run("is_printable_ascii", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_printable_ascii()) }`, "v", []sub{
			{"valid/text", "Hello, World!", true},
			{"valid/symbols", "~!@#$%^&*()", true},
			{"invalid/tab", "hello\tworld", false},
			{"invalid/null-byte", "hi\x00there", false},
			{"edge/empty", "", true},
		})
	})

	t.Run("is_multibyte", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_multibyte()) }`, "v", []sub{
			{"valid/chinese", "你好", true},
			{"valid/emoji", "🎉", true},
			{"invalid/ascii", "hello", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_alpha", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_alpha()) }`, "v", []sub{
			{"valid/lower", "abc", true},
			{"valid/upper", "XYZ", true},
			{"valid/mixed", "Hello", true},
			{"invalid/digits", "abc123", false},
			{"invalid/spaces", "abc def", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_alpha_num", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_alpha_num()) }`, "v", []sub{
			{"valid/mixed", "Hello123", true},
			{"valid/digits", "12345", true},
			{"invalid/dash", "abc-123", false},
			{"invalid/space", "abc 123", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_alpha_dash", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_alpha_dash()) }`, "v", []sub{
			{"valid/slug", "hello-world_123", true},
			{"valid/underscore", "my_var", true},
			{"invalid/space", "hello world", false},
			{"invalid/special", "foo@bar", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_numeric", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_numeric()) }`, "v", []sub{
			{"valid/digits", "123456", true},
			{"valid/zero", "0", true},
			{"invalid/decimal", "12.34", false},
			{"invalid/alpha", "12a34", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_number", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_number()) }`, "v", []sub{
			{"valid/int", "42", true},
			{"valid/decimal", "3.14", true},
			{"valid/negative", "-10", true},
			{"invalid/alpha", "abc", false},
			{"invalid/multi-dot", "1.2.3", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_hex", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_hex()) }`, "v", []sub{
			{"valid/lower", "deadbeef", true},
			{"valid/upper", "DEADBEEF", true},
			{"valid/mixed", "0123abcDEF", true},
			{"invalid/g", "0123g", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_hex_color", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_hex_color()) }`, "v", []sub{
			{"valid/6-digit", "#FF5733", true},
			{"valid/3-digit", "#FFF", true},
			{"invalid/no-hash", "FF5733", false},
			{"invalid/7-digit", "#FF57331", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_rgb_color", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_rgb_color()) }`, "v", []sub{
			{"valid/normal", "rgb(255,128,0)", true},
			{"valid/zeros", "rgb(0,0,0)", true},
			{"invalid/no-prefix", "255,128,0", false},
			{"invalid/text", "rgb(a,b,c)", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_dns_name", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_dns_name()) }`, "v", []sub{
			{"valid/domain", "example.com", true},
			{"valid/subdomain", "sub.domain.org", true},
			{"invalid/space", "inva lid.com", false},
			{"invalid/dot-only", ".", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_data_uri", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_data_uri()) }`, "v", []sub{
			{"valid/text", "data:text/plain;base64,SGVsbG8=", true},
			{"valid/image", "data:image/png;base64,iVBOR", true},
			{"invalid/no-prefix", "text/plain;base64,SGVsbG8=", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_latitude", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_latitude()) }`, "v", []sub{
			{"valid/positive", "45.0", true},
			{"valid/negative", "-89.99", true},
			{"valid/boundary", "90", true},
			{"invalid/over", "91.0", false},
			{"invalid/text", "north", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_longitude", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_longitude()) }`, "v", []sub{
			{"valid/positive", "120.5", true},
			{"valid/negative", "-179.99", true},
			{"valid/boundary", "180", true},
			{"invalid/over", "181.0", false},
			{"invalid/text", "east", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_isbn10", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_isbn10()) }`, "v", []sub{
			{"valid/standard", "0306406152", true},
			{"valid/with-X", "080442957X", true},
			{"invalid/too-short", "12345", false},
			{"invalid/alpha", "abcdefghij", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_isbn13", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_isbn13()) }`, "v", []sub{
			{"valid/978", "9780306406157", true},
			{"invalid/too-short", "978030640615", false},
			{"invalid/alpha", "978abcdefghij", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("not_blank", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.not_blank()) }`, "v", []sub{
			{"valid/text", "hello", true},
			{"valid/with-space", " x ", true},
			{"invalid/empty", "", false},
			{"invalid/spaces", "   ", false},
			{"invalid/tabs", "\t\n", false},
		})
	})

	t.Run("has_whitespace", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.has_whitespace()) }`, "v", []sub{
			{"valid/space", "hello world", true},
			{"valid/tab", "hello\tworld", true},
			{"invalid/no-space", "helloworld", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_cn_mobile", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.is_cn_mobile()) }`, "v", []sub{
			{"valid/13x", "13800138000", true},
			{"valid/19x", "19912345678", true},
			{"invalid/short", "1380013800", false},
			{"invalid/non-1", "23800138000", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("luhn_valid", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.luhn_valid()) }`, "v", []sub{
			{"valid/visa", "4111111111111111", true},
			{"valid/mastercard", "5500000000000004", true},
			{"valid/discover", "6011000000000004", true},
			{"invalid/bad-checksum", "4111111111111112", false},
			{"invalid/alpha", "abcd", false},
			{"edge/empty", "", false},
		})
	})

	// ===== Parameterized Methods =====

	t.Run("between", func(t *testing.T) {
		run(t, `{ v: int, c: bool @blob(this.v.between(min: 1, max: 100)) }`, "v", []sub{
			{"valid/mid", int64(50), true},
			{"valid/min-boundary", int64(1), true},
			{"valid/max-boundary", int64(100), true},
			{"invalid/below", int64(0), false},
			{"invalid/above", int64(101), false},
		})
	})

	t.Run("len_between", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.len_between(min: 3, max: 10)) }`, "v", []sub{
			{"valid/exact-min", "abc", true},
			{"valid/exact-max", "0123456789", true},
			{"valid/mid", "hello", true},
			{"invalid/too-short", "ab", false},
			{"invalid/too-long", "01234567890", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("min_len", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.min_len(n: 3)) }`, "v", []sub{
			{"valid/exact", "abc", true},
			{"valid/longer", "hello", true},
			{"invalid/short", "ab", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("max_len", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.max_len(n: 5)) }`, "v", []sub{
			{"valid/exact", "abcde", true},
			{"valid/shorter", "hi", true},
			{"invalid/too-long", "toolong", false},
			{"edge/empty", "", true},
		})
	})

	t.Run("str_len", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(this.v.str_len(min: 2, max: 5)) }`, "v", []sub{
			{"valid/ascii", "hi", true},
			{"valid/unicode", "你好", true}, // 2 runes
			{"valid/max", "abcde", true},
			{"invalid/too-short", "x", false},
			{"invalid/too-long", "abcdef", false},
			{"edge/empty", "", false},
		})
	})

	// ===== Built-in Functions =====

	t.Run("is_valid_date", func(t *testing.T) {
		run(t, `{ v: string, c: bool @blob(is_valid_date(this.v)) }`, "v", []sub{
			{"valid/iso", "2024-01-15", true},
			{"valid/rfc3339", "2024-01-15T10:30:00Z", true},
			{"valid/slash", "2024/01/15", true},
			{"invalid/text", "not-a-date", false},
			{"invalid/month-13", "2024-13-01", false},
			{"edge/empty", "", false},
		})
	})

	t.Run("is_past_date", func(t *testing.T) {
		v := MustNew(`{ v: string, c: bool @blob(is_past_date(this.v)) }`)
		cases := []sub{
			{"valid/past", "2020-01-01", true},
			{"valid/far-past", "1999-12-31", true},
			{"invalid/future", "2099-01-01", false},
			{"invalid/not-date", "not-a-date", false},
			{"edge/empty", "", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				r := v.Process(map[string]any{"v": tc.input})
				if r.Valid != tc.valid {
					t.Errorf("is_past_date(%v): got valid=%v, want %v; errors=%v",
						tc.input, r.Valid, tc.valid, r.Errors)
				}
			})
		}
	})

	t.Run("is_future_date", func(t *testing.T) {
		v := MustNew(`{ v: string, c: bool @blob(is_future_date(this.v)) }`)
		cases := []sub{
			{"valid/far-future", "2099-01-01", true},
			{"valid/next-century", "2199-12-31", true},
			{"invalid/past", "2020-01-01", false},
			{"invalid/not-date", "not-a-date", false},
			{"edge/empty", "", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				r := v.Process(map[string]any{"v": tc.input})
				if r.Valid != tc.valid {
					t.Errorf("is_future_date(%v): got valid=%v, want %v; errors=%v",
						tc.input, r.Valid, tc.valid, r.Errors)
				}
			})
		}
	})

	t.Run("in_list", func(t *testing.T) {
		v := MustNew(`{ v: string, c: bool @blob(in_list(this.v, ["active","pending","done"])) }`)
		cases := []sub{
			{"valid/active", "active", true},
			{"valid/pending", "pending", true},
			{"invalid/unknown", "unknown", false},
			{"edge/empty", "", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				r := v.Process(map[string]any{"v": tc.input})
				if r.Valid != tc.valid {
					t.Errorf("in_list(%v): got valid=%v, want %v; errors=%v",
						tc.input, r.Valid, tc.valid, r.Errors)
				}
			})
		}
	})
}
