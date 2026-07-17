package schemix

import "testing"

func TestBuiltinMethod_IsEmail(t *testing.T) {
	v := MustNew(`{ email: string, check: bool @blob(this.email.is_email()) }`)

	tests := []struct{ input string; valid bool }{
		{"alice@example.com", true},
		{"user.name+tag@domain.co", true},
		{"invalid", false},
		{"@missing.com", false},
		{"no-domain@", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"email": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_email(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_IsURL(t *testing.T) {
	v := MustNew(`{ url: string, check: bool @blob(this.url.is_url()) }`)

	tests := []struct{ input string; valid bool }{
		{"https://example.com", true},
		{"http://localhost:8080/path", true},
		{"ftp://files.example.com/doc.pdf", true},
		{"example.com", false},  // no scheme
		{"not a url", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"url": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_url(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_IsUUID(t *testing.T) {
	v := MustNew(`{ id: string, check: bool @blob(this.id.is_uuid()) }`)

	tests := []struct{ input string; valid bool }{
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"550E8400-E29B-41D4-A716-446655440000", true},
		{"not-a-uuid", false},
		{"550e8400e29b41d4a716446655440000", false}, // no dashes
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"id": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_uuid(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_IsIP(t *testing.T) {
	vIPv4 := MustNew(`{ ip: string, check: bool @blob(this.ip.is_ipv4()) }`)
	vIPv6 := MustNew(`{ ip: string, check: bool @blob(this.ip.is_ipv6()) }`)
	vIP := MustNew(`{ ip: string, check: bool @blob(this.ip.is_ip()) }`)

	// IPv4
	r := vIPv4.Process(map[string]any{"ip": "192.168.1.1"})
	if !r.Valid {
		t.Error("192.168.1.1 should be valid IPv4")
	}
	r = vIPv4.Process(map[string]any{"ip": "::1"})
	if r.Valid {
		t.Error("::1 should NOT be valid IPv4")
	}

	// IPv6
	r = vIPv6.Process(map[string]any{"ip": "::1"})
	if !r.Valid {
		t.Error("::1 should be valid IPv6")
	}
	r = vIPv6.Process(map[string]any{"ip": "192.168.1.1"})
	if r.Valid {
		t.Error("192.168.1.1 should NOT be valid IPv6")
	}

	// Any IP
	r = vIP.Process(map[string]any{"ip": "192.168.1.1"})
	if !r.Valid {
		t.Error("192.168.1.1 should be valid IP")
	}
	r = vIP.Process(map[string]any{"ip": "::1"})
	if !r.Valid {
		t.Error("::1 should be valid IP")
	}
	r = vIP.Process(map[string]any{"ip": "not-an-ip"})
	if r.Valid {
		t.Error("not-an-ip should be invalid IP")
	}
}

func TestBuiltinMethod_LuhnValid(t *testing.T) {
	v := MustNew(`{ pan: string, check: bool @blob(this.pan.luhn_valid()) }`)

	tests := []struct{ input string; valid bool }{
		{"4111111111111111", true},  // Visa test card
		{"5500000000000004", true},  // Mastercard test card
		{"6011000000000004", true},  // Discover test card
		{"4111111111111112", false}, // invalid checksum
		{"1234567890123456", false},
		{"abcd", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"pan": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("luhn_valid(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_NotBlank(t *testing.T) {
	v := MustNew(`{ s: string, check: bool @blob(this.s.not_blank()) }`)

	tests := []struct{ input string; valid bool }{
		{"hello", true},
		{" x ", true},
		{"", false},
		{"   ", false},
		{"\t\n", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"s": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("not_blank(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_IsNumeric(t *testing.T) {
	v := MustNew(`{ s: string, check: bool @blob(this.s.is_numeric()) }`)

	tests := []struct{ input string; valid bool }{
		{"12345", true},
		{"0", true},
		{"12.34", false},
		{"12a34", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"s": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_numeric(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_IsAlpha(t *testing.T) {
	v := MustNew(`{ s: string, check: bool @blob(this.s.is_alpha()) }`)

	tests := []struct{ input string; valid bool }{
		{"Hello", true},
		{"abc", true},
		{"abc123", false},
		{"abc def", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"s": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_alpha(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_IsAlphanumeric(t *testing.T) {
	v := MustNew(`{ s: string, check: bool @blob(this.s.is_alpha_num()) }`)

	tests := []struct{ input string; valid bool }{
		{"Hello123", true},
		{"abc", true},
		{"123", true},
		{"abc-123", false},
		{"abc 123", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"s": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_alpha_num(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_IsJSON(t *testing.T) {
	v := MustNew(`{ s: string, check: bool @blob(this.s.is_json()) }`)

	tests := []struct{ input string; valid bool }{
		{`{"key":"value"}`, true},
		{`[1,2,3]`, true},
		{`"hello"`, true},
		{`null`, true},
		{`{invalid}`, false},
		{``, false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"s": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_json(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_Between(t *testing.T) {
	v := MustNew(`{ n: int, check: bool @blob(this.n.between(min: 1, max: 100)) }`)

	tests := []struct{ input int64; valid bool }{
		{1, true},
		{50, true},
		{100, true},
		{0, false},
		{101, false},
		{-1, false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"n": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("between(%d): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinMethod_LenBetween(t *testing.T) {
	v := MustNew(`{ s: string, check: bool @blob(this.s.len_between(min: 3, max: 10)) }`)

	tests := []struct{ input string; valid bool }{
		{"abc", true},
		{"hello", true},
		{"0123456789", true},
		{"ab", false},
		{"01234567890", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"s": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("len_between(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}

func TestBuiltinFunc_IsValidDate(t *testing.T) {
	v := MustNew(`{ d: string, check: bool @blob(is_valid_date(this.d)) }`)

	tests := []struct{ input string; valid bool }{
		{"2024-01-15", true},
		{"2024-01-15T10:30:00Z", true},
		{"2024/01/15", true},
		{"not-a-date", false},
		{"2024-13-01", false},
		{"", false},
	}
	for _, tt := range tests {
		r := v.Process(map[string]any{"d": tt.input})
		if r.Valid != tt.valid {
			t.Errorf("is_valid_date(%q): got valid=%v, want %v", tt.input, r.Valid, tt.valid)
		}
	}
}
