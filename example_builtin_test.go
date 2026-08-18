package schemix_test

import (
	"fmt"
	"sort"

	"github.com/mredencom/schemix"
)

// The 37+ built-in validators are available inside @blob() with no
// registration. They are methods on the value: this.field.is_email().
func ExampleNew_builtinFormatValidators() {
	v := schemix.MustNew(`{
		email:     string @blob(this.email.is_email())
		url:       string @blob(this.url.is_full_url())
		ip:        string @blob(this.ip.is_ipv4())
		uuid:      string @blob(this.uuid.is_uuid4())
		dns:       string @blob(this.dns.is_dns_name())
		mac:       string @blob(this.mac.is_mac())
		cn_mobile: string @blob(this.cn_mobile.is_cn_mobile())
	}`)

	good := map[string]any{
		"email":     "user@example.com",
		"url":       "https://api.example.com/v1",
		"ip":        "192.168.1.1",
		"uuid":      "550e8400-e29b-41d4-a716-446655440000",
		"dns":       "api.example.com",
		"mac":       "01:23:45:67:89:AB",
		"cn_mobile": "13800138000",
	}
	fmt.Println("all valid:", v.ProcessWithMode(good, schemix.FailAll).Valid)

	bad := map[string]any{
		"email":     "invalid",
		"url":       "not-a-url",
		"ip":        "999.999.999.999",
		"uuid":      "not-uuid",
		"dns":       "-invalid",
		"mac":       "ZZ:ZZ:ZZ",
		"cn_mobile": "12345",
	}
	r := v.ProcessWithMode(bad, schemix.FailAll)
	fmt.Println("failing fields:", len(r.Errors))
	// Output:
	// all valid: true
	// failing fields: 7
}

// Character-class and length validators. Length helpers work on strings,
// slices, and maps.
func ExampleNew_builtinLengthValidators() {
	v := schemix.MustNew(`{
		code:     string @blob(this.code.is_alpha_num())
		slug:     string @blob(this.slug.is_alpha_dash())
		pin:      string @blob(this.pin.is_numeric())
		password: string @blob(this.password.len_between(min: 8, max: 64))
		age:      int    @blob(this.age.between(min: 13, max: 150))
	}`)

	fmt.Println("valid:", v.ProcessWithMode(map[string]any{
		"code": "ABC123", "slug": "my-post_2024", "pin": "123456",
		"password": "s3cret-passphrase", "age": int64(28),
	}, schemix.FailAll).Valid)

	r := v.ProcessWithMode(map[string]any{
		"code": "ABC 123", "slug": "has space", "pin": "12.34",
		"password": "short", "age": int64(200),
	}, schemix.FailAll)

	paths := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)
	fmt.Println("failing:", paths)
	// Output:
	// valid: true
	// failing: [age code password pin slug]
}

// luhn_valid implements the checksum used by payment card numbers.
func ExampleNew_builtinLuhn() {
	v := schemix.MustNew(`{
		pan:  =~"^[0-9]{16}$"
		luhn: bool @blob(this.pan.luhn_valid())
	}`)

	fmt.Println(v.Process(map[string]any{"pan": "4111111111111111"}).Valid)
	fmt.Println(v.Process(map[string]any{"pan": "4111111111111112"}).Valid)
	// Output:
	// true
	// false
}

// Date and list helpers are functions rather than methods: they take the value
// as an argument.
func ExampleNew_builtinFunctions() {
	v := schemix.MustNew(`{
		birthday: string @blob(is_valid_date(this.birthday) && is_past_date(this.birthday))
		expiry:   string @blob(is_future_date(this.expiry))
		status:   string @blob(in_list(this.status, ["active", "pending"]))
	}`)

	fmt.Println("valid:", v.ProcessWithMode(map[string]any{
		"birthday": "1990-05-20", "expiry": "2999-01-01", "status": "active",
	}, schemix.FailAll).Valid)

	r := v.ProcessWithMode(map[string]any{
		"birthday": "2999-01-01", "expiry": "1990-05-20", "status": "deleted",
	}, schemix.FailAll)
	fmt.Println("failing:", len(r.Errors))
	// Output:
	// valid: true
	// failing: 3
}
