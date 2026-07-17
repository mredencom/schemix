package main

import (
	"fmt"

	"github.com/mredencom/schemix"
)

func builtinValidatorsExample() {
	// === String format validators ===
	fmt.Println("  String format validators:")

	vFormat := schemix.MustNew(`{
		email:     string @blob(this.email.is_email())
		url:       string @blob(this.url.is_full_url())
		ip:        string @blob(this.ip.is_ipv4())
		uuid:      string @blob(this.uuid.is_uuid4())
		dns:       string @blob(this.dns.is_dns_name())
		mac:       string @blob(this.mac.is_mac())
		cn_mobile: string @blob(this.cn_mobile.is_cn_mobile())
	}`)

	r := vFormat.Process(map[string]any{
		"email":     "user@example.com",
		"url":       "https://api.example.com/v1",
		"ip":        "192.168.1.1",
		"uuid":      "550e8400-e29b-41d4-a716-446655440000",
		"dns":       "api.example.com",
		"mac":       "01:23:45:67:89:AB",
		"cn_mobile": "13800138000",
	})
	fmt.Printf("    all valid: %v\n", r.Valid)

	r = vFormat.Process(map[string]any{
		"email":     "invalid",
		"url":       "not-a-url",
		"ip":        "999.999.999.999",
		"uuid":      "not-uuid",
		"dns":       "-invalid",
		"mac":       "ZZ:ZZ:ZZ",
		"cn_mobile": "12345",
	})
	fmt.Printf("    all invalid: errors=%d\n", len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("      %s failed\n", e.Path)
	}

	// === Character type validators ===
	fmt.Println("\n  Character type validators:")

	vChar := schemix.MustNew(`{
		code:  string @blob(this.code.is_alpha_num())
		slug:  string @blob(this.slug.is_alpha_dash())
		pin:   string @blob(this.pin.is_numeric())
		ascii: string @blob(this.ascii.is_ascii())
	}`)

	r = vChar.Process(map[string]any{
		"code": "ABC123", "slug": "my-post_2024", "pin": "123456", "ascii": "Hello!",
	})
	fmt.Printf("    valid: %v\n", r.Valid)

	r = vChar.Process(map[string]any{
		"code": "ABC 123", "slug": "has space", "pin": "12.34", "ascii": "你好",
	})
	fmt.Printf("    invalid: errors=%d\n", len(r.Errors))

	// === Length validators ===
	fmt.Println("\n  Length validators:")

	vLen := schemix.MustNew(`{
		username: string @blob(this.username.len_between(min: 3, max: 20))
		password: string @blob(this.password.min_len(n: 8))
		pin:      string @blob(this.pin.max_len(n: 6))
		nickname: string @blob(this.nickname.str_len(min: 2, max: 10))
	}`)

	r = vLen.Process(map[string]any{
		"username": "alice_dev", "password": "secure123!", "pin": "1234", "nickname": "小明",
	})
	fmt.Printf("    valid: %v\n", r.Valid)

	r = vLen.Process(map[string]any{
		"username": "ab", "password": "short", "pin": "1234567", "nickname": "a",
	})
	fmt.Printf("    invalid: errors=%d\n", len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("      %s failed\n", e.Path)
	}

	// === Numeric range ===
	fmt.Println("\n  Numeric range validator:")

	vNum := schemix.MustNew(`{
		age:      int    @blob(this.age.between(min: 0, max: 150))
		score:    float  @blob(this.score.between(min: 0, max: 100))
		quantity: int    @blob(this.quantity.between(min: 1, max: 999))
	}`)

	r = vNum.Process(map[string]any{"age": int64(28), "score": 95.5, "quantity": int64(10)})
	fmt.Printf("    valid: %v\n", r.Valid)

	r = vNum.Process(map[string]any{"age": int64(200), "score": -1.0, "quantity": int64(0)})
	fmt.Printf("    invalid: errors=%d\n", len(r.Errors))

	// === Financial / Card ===
	fmt.Println("\n  Financial validators:")

	vCard := schemix.MustNew(`{
		pan:     =~"^[0-9]{13,19}$"
		luhn:    bool @blob(this.pan.luhn_valid())
		amount:  int  @blob(this.amount.between(min: 1, max: 10000000))
	}`)

	r = vCard.Process(map[string]any{"pan": "4111111111111111", "amount": int64(5000)})
	fmt.Printf("    Visa test card: valid=%v\n", r.Valid)

	r = vCard.Process(map[string]any{"pan": "4111111111111112", "amount": int64(5000)})
	fmt.Printf("    Bad checksum: valid=%v (luhn failed)\n", r.Valid)

	// === Data format validators ===
	fmt.Println("\n  Data format validators:")

	vData := schemix.MustNew(`{
		payload: string @blob(this.payload.is_json())
		token:   string @blob(this.token.is_base64())
		color:   string @blob(this.color.is_hex_color())
	}`)

	r = vData.Process(map[string]any{
		"payload": `{"key":"value"}`,
		"token":   "SGVsbG8gV29ybGQ=",
		"color":   "#FF5733",
	})
	fmt.Printf("    valid: %v\n", r.Valid)

	// === Date validators (function style) ===
	fmt.Println("\n  Date validators:")

	vDate := schemix.MustNew(`{
		birthday:   string @blob(is_valid_date(this.birthday))
		birth_past: bool   @blob(is_past_date(this.birthday))
		expiry:     string @blob(is_valid_date(this.expiry))
		exp_future: bool   @blob(is_future_date(this.expiry))
	}`)

	r = vDate.Process(map[string]any{
		"birthday": "1990-05-15",
		"expiry":   "2030-12-31",
	})
	fmt.Printf("    valid dates: %v\n", r.Valid)

	r = vDate.Process(map[string]any{
		"birthday": "not-a-date",
		"expiry":   "also-invalid",
	})
	fmt.Printf("    invalid dates: errors=%d\n", len(r.Errors))

	// === Combined: real-world user registration ===
	fmt.Println("\n  Real-world example (user registration):")

	vUser := schemix.MustNew(`{
		username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,20}$"
		email:    string @blob(this.email.is_email())
		password: string @blob(this.password.len_between(min: 8, max: 64))
		phone?:   string @meta(optional) @blob(this.phone.is_cn_mobile())
		age:      int    @blob(this.age.between(min: 13, max: 150))
		avatar?:  string @meta(optional) @blob(this.avatar.is_full_url())
	}`)

	r = vUser.Process(map[string]any{
		"username": "alice_dev",
		"email":    "alice@example.com",
		"password": "MyP@ssw0rd!",
		"phone":    "13800138000",
		"age":      int64(28),
		"avatar":   "https://cdn.example.com/avatars/alice.jpg",
	})
	fmt.Printf("    complete profile: valid=%v\n", r.Valid)

	r = vUser.Process(map[string]any{
		"username": "alice_dev",
		"email":    "alice@example.com",
		"password": "secure123",
		"age":      int64(25),
	})
	fmt.Printf("    minimal profile (no phone/avatar): valid=%v\n", r.Valid)

	r = vUser.Process(map[string]any{
		"username": "x",
		"email":    "bad",
		"password": "short",
		"age":      int64(5),
	})
	fmt.Printf("    all bad: valid=%v, errors=%d\n", r.Valid, len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("      [%s] %s\n", e.Code, e.Path)
	}
}
