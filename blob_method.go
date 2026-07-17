package schemix

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

// builtinMethods returns all built-in validation methods to register.
// These methods cover format validations that are cumbersome to express
// with CUE regex or Bloblang alone.
//
// Methods already covered by CUE/Bloblang native features are NOT included:
//   - Type checks (int/string/bool) → use CUE type constraints
//   - Range/min/max → use CUE: `int & >=0 & <=100`
//   - Enum/in → use CUE: `"a" | "b" | "c"`
//   - Regex → use CUE: `=~"pattern"`
//   - contains/starts_with/ends_with → Bloblang: has_prefix/has_suffix/contains
//   - length → Bloblang: this.field.length()
//   - Field comparison → @blob: this.a > this.b
func builtinMethods() []struct {
	name string
	spec *bloblang.PluginSpec
	ctor bloblang.MethodConstructorV2
} {
	return []struct {
		name string
		spec *bloblang.PluginSpec
		ctor bloblang.MethodConstructorV2
	}{
		// ===== String format validation =====

		{"is_email", specValidation("Validates email address format"), noParamMethod(methodIsEmail)},
		{"is_url", specValidation("Validates URL with scheme (http/https/ftp...)"), noParamMethod(methodIsURL)},
		{"is_full_url", specValidation("Validates full URL (must start with http:// or https://)"), noParamMethod(methodIsFullURL)},
		{"is_uuid", specValidation("Validates UUID (v1-v5, any case)"), noParamMethod(methodIsUUID)},
		{"is_uuid3", specValidation("Validates UUID v3"), noParamMethod(methodIsUUID3)},
		{"is_uuid4", specValidation("Validates UUID v4"), noParamMethod(methodIsUUID4)},
		{"is_uuid5", specValidation("Validates UUID v5"), noParamMethod(methodIsUUID5)},
		{"is_ip", specValidation("Validates IP address (v4 or v6)"), noParamMethod(methodIsIP)},
		{"is_ipv4", specValidation("Validates IPv4 address"), noParamMethod(methodIsIPv4)},
		{"is_ipv6", specValidation("Validates IPv6 address"), noParamMethod(methodIsIPv6)},
		{"is_cidr", specValidation("Validates CIDR notation (v4 or v6)"), noParamMethod(methodIsCIDR)},
		{"is_mac", specValidation("Validates MAC address"), noParamMethod(methodIsMAC)},
		{"is_json", specValidation("Validates JSON string"), noParamMethod(methodIsJSON)},
		{"is_base64", specValidation("Validates Base64 encoded string"), noParamMethod(methodIsBase64)},
		{"is_ascii", specValidation("Validates ASCII-only string"), noParamMethod(methodIsASCII)},
		{"is_printable_ascii", specValidation("Validates printable ASCII string"), noParamMethod(methodIsPrintableASCII)},
		{"is_multibyte", specValidation("Validates string contains multibyte characters"), noParamMethod(methodIsMultibyte)},
		{"is_alpha", specValidation("Validates alphabetic characters only"), noParamMethod(methodIsAlpha)},
		{"is_alpha_num", specValidation("Validates alphanumeric characters only"), noParamMethod(methodIsAlphanumeric)},
		{"is_alpha_dash", specValidation("Validates letters, digits, dashes, underscores only"), noParamMethod(methodIsAlphaDash)},
		{"is_numeric", specValidation("Validates digit-only string (0-9)"), noParamMethod(methodIsNumeric)},
		{"is_number", specValidation("Validates number string (digits, optional leading sign, decimal point)"), noParamMethod(methodIsNumber)},
		{"is_hex", specValidation("Validates hexadecimal string"), noParamMethod(methodIsHex)},
		{"is_hex_color", specValidation("Validates hex color (#RGB or #RRGGBB)"), noParamMethod(methodIsHexColor)},
		{"is_rgb_color", specValidation("Validates RGB color string rgb(r,g,b)"), noParamMethod(methodIsRGBColor)},
		{"is_dns_name", specValidation("Validates DNS name"), noParamMethod(methodIsDNSName)},
		{"is_data_uri", specValidation("Validates data URI (data:mime;base64,...)"), noParamMethod(methodIsDataURI)},
		{"is_latitude", specValidation("Validates latitude value (-90 to 90)"), noParamMethod(methodIsLatitude)},
		{"is_longitude", specValidation("Validates longitude value (-180 to 180)"), noParamMethod(methodIsLongitude)},
		{"is_isbn10", specValidation("Validates ISBN-10 format"), noParamMethod(methodIsISBN10)},
		{"is_isbn13", specValidation("Validates ISBN-13 format"), noParamMethod(methodIsISBN13)},
		{"not_blank", specValidation("Validates string is not empty or whitespace-only"), noParamMethod(methodNotBlank)},
		{"has_whitespace", specValidation("Checks string contains whitespace"), noParamMethod(methodHasWhitespace)},

		// ===== China-specific =====

		{"is_cn_mobile", specValidation("Validates China mobile number (1xx-xxxx-xxxx)"), noParamMethod(methodIsCnMobile)},

		// ===== Card/Financial =====

		{"luhn_valid", specValidation("Validates Luhn checksum (credit card numbers)"), noParamMethod(methodLuhnValid)},

		// ===== Parameterized methods =====

		{
			name: "between",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Validates number is between min and max (inclusive)").
				Param(bloblang.NewFloat64Param("min")).Param(bloblang.NewFloat64Param("max")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				min, _ := args.GetFloat64("min")
				max, _ := args.GetFloat64("max")
				return func(v any) (any, error) {
					n, ok := toFloat64(v)
					if !ok {
						return false, nil
					}
					return n >= min && n <= max, nil
				}, nil
			},
		},
		{
			name: "len_between",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Validates string/slice/map length is between min and max (inclusive)").
				Param(bloblang.NewInt64Param("min")).Param(bloblang.NewInt64Param("max")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				min, _ := args.GetInt64("min")
				max, _ := args.GetInt64("max")
				return func(v any) (any, error) {
					l := valueLen(v)
					if l < 0 {
						return false, nil
					}
					return int64(l) >= min && int64(l) <= max, nil
				}, nil
			},
		},
		{
			name: "min_len",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Validates minimum length of string/slice/map").
				Param(bloblang.NewInt64Param("n")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				n, _ := args.GetInt64("n")
				return func(v any) (any, error) {
					l := valueLen(v)
					if l < 0 {
						return false, nil
					}
					return int64(l) >= n, nil
				}, nil
			},
		},
		{
			name: "max_len",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Validates maximum length of string/slice/map").
				Param(bloblang.NewInt64Param("n")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				n, _ := args.GetInt64("n")
				return func(v any) (any, error) {
					l := valueLen(v)
					if l < 0 {
						return false, nil
					}
					return int64(l) <= n, nil
				}, nil
			},
		},
		{
			name: "str_len",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Validates string character length (rune count) is between min and max").
				Param(bloblang.NewInt64Param("min")).Param(bloblang.NewInt64Param("max")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				min, _ := args.GetInt64("min")
				max, _ := args.GetInt64("max")
				return func(v any) (any, error) {
					s, ok := v.(string)
					if !ok {
						return false, nil
					}
					l := int64(utf8.RuneCountInString(s))
					return l >= min && l <= max, nil
				}, nil
			},
		},
	}
}

// --- Helpers ---

func specValidation(desc string) *bloblang.PluginSpec {
	return bloblang.NewPluginSpec().Category(categoryValidation).Description(desc)
}

func noParamMethod(fn bloblang.Method) bloblang.MethodConstructorV2 {
	return func(_ *bloblang.ParsedParams) (bloblang.Method, error) {
		return fn, nil
	}
}

func valueLen(v any) int {
	switch val := v.(type) {
	case string:
		return len(val)
	case []any:
		return len(val)
	case map[string]any:
		return len(val)
	default:
		return -1
	}
}

// --- Method implementations ---

// Pre-compiled regexes for format validation (compiled once at package init).
var (
	reEmail        = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	reUUID         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reUUID3        = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-3[0-9a-fA-F]{3}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reUUID4        = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	reUUID5        = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-5[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	reHexColor     = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	reRGBColor     = regexp.MustCompile(`^rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)$`)
	reDNSName      = regexp.MustCompile(`^([a-zA-Z0-9_]{1}[a-zA-Z0-9_-]{0,62})(\.[a-zA-Z0-9_]{1}[a-zA-Z0-9_-]{0,62})*?(\.[a-zA-Z]{2,})$`)
	reDataURI      = regexp.MustCompile(`^data:[a-z]+/[a-z0-9\-+.]+;base64,`)
	reLatitude     = regexp.MustCompile(`^[-+]?([1-8]?\d(\.\d+)?|90(\.0+)?)$`)
	reLongitude    = regexp.MustCompile(`^[-+]?(180(\.0+)?|((1[0-7]\d)|([1-9]?\d))(\.\d+)?)$`)
	reISBN10       = regexp.MustCompile(`^(?:\d[\ -]?){9}[\dxX]$`)
	reISBN13       = regexp.MustCompile(`^(?:\d[\ -]?){13}$`)
	reCnMobile     = regexp.MustCompile(`^1[3-9]\d{9}$`)
	reNumber       = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)$`)
	reHex          = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	reAlphaDash    = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

func methodIsEmail(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reEmail.MatchString(s), nil
}

func methodIsURL(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Host != "", nil
}

func methodIsFullURL(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false, nil
	}
	return u.Scheme == "http" || u.Scheme == "https", nil
}

func methodIsUUID(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reUUID.MatchString(s), nil
}

func methodIsUUID3(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reUUID3.MatchString(s), nil
}

func methodIsUUID4(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reUUID4.MatchString(s), nil
}

func methodIsUUID5(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reUUID5.MatchString(s), nil
}

func methodIsIP(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return net.ParseIP(s) != nil, nil
}

func methodIsIPv4(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil, nil
}

func methodIsIPv6(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() == nil, nil
}

func methodIsCIDR(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil, nil
}

func methodIsMAC(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	_, err := net.ParseMAC(s)
	return err == nil, nil
}

func methodIsJSON(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return json.Valid([]byte(s)), nil
}

func methodIsBase64(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	if s == "" {
		return false, nil
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil, nil
}

func methodIsASCII(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false, nil
		}
	}
	return true, nil
}

func methodIsPrintableASCII(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	for _, r := range s {
		if r < 32 || r > 126 {
			return false, nil
		}
	}
	return true, nil
}

func methodIsMultibyte(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	for _, r := range s {
		if r > unicode.MaxASCII {
			return true, nil
		}
	}
	return false, nil
}

func methodIsAlpha(v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return false, nil
	}
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false, nil
		}
	}
	return true, nil
}

func methodIsAlphanumeric(v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return false, nil
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false, nil
		}
	}
	return true, nil
}

func methodIsAlphaDash(v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return false, nil
	}
	return reAlphaDash.MatchString(s), nil
}

func methodIsNumeric(v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return false, nil
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false, nil
		}
	}
	return true, nil
}

func methodIsNumber(v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return false, nil
	}
	return reNumber.MatchString(s), nil
}

func methodIsHex(v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return false, nil
	}
	return reHex.MatchString(s), nil
}

func methodIsHexColor(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reHexColor.MatchString(s), nil
}

func methodIsRGBColor(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reRGBColor.MatchString(s), nil
}

func methodIsDNSName(v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return false, nil
	}
	if len(s) > 253 {
		return false, nil
	}
	return reDNSName.MatchString(s), nil
}

func methodIsDataURI(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reDataURI.MatchString(s), nil
}

func methodIsLatitude(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reLatitude.MatchString(s), nil
}

func methodIsLongitude(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reLongitude.MatchString(s), nil
}

func methodIsISBN10(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reISBN10.MatchString(strings.ReplaceAll(s, " ", "")), nil
}

func methodIsISBN13(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reISBN13.MatchString(strings.ReplaceAll(s, " ", "")), nil
}

func methodIsCnMobile(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return reCnMobile.MatchString(s), nil
}

func methodNotBlank(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	return strings.TrimSpace(s) != "", nil
}

func methodHasWhitespace(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return true, nil
		}
	}
	return false, nil
}

func methodLuhnValid(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return false, nil
	}
	if len(s) < 2 {
		return false, nil
	}
	sum := 0
	alt := false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			return false, nil
		}
		n := int(c - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0, nil
}
