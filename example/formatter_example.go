package main

import (
	"fmt"

	"github.com/mredencom/schemix"
)

// i18n error messages — simulate a multi-language error formatter
var errorMessages = map[string]map[schemix.ErrorCode]string{
	"zh-CN": {
		schemix.CodeRequiredMissing: "此字段为必填项",
		schemix.CodeTypeMismatch:    "数据类型错误",
		schemix.CodeFormatMismatch:  "格式不正确",
		schemix.CodeEnumInvalid:     "值不在允许范围内",
		schemix.CodeRangeViolation:  "数值超出范围",
		schemix.CodeBizRuleFailed:   "业务规则校验失败",
		schemix.CodeCondRequired:    "条件必填未满足",
		schemix.CodeExprExecError:   "表达式执行错误",
	},
	"en": {
		schemix.CodeRequiredMissing: "This field is required",
		schemix.CodeTypeMismatch:    "Invalid data type",
		schemix.CodeFormatMismatch:  "Invalid format",
		schemix.CodeEnumInvalid:     "Value not allowed",
		schemix.CodeRangeViolation:  "Value out of range",
		schemix.CodeBizRuleFailed:   "Business rule failed",
		schemix.CodeCondRequired:    "Conditionally required",
		schemix.CodeExprExecError:   "Expression error",
	},
}

func i18nFormatter(lang string) schemix.ErrorFormatter {
	msgs := errorMessages[lang]
	return func(code schemix.ErrorCode, path, detail string) string {
		if msg, ok := msgs[code]; ok {
			return fmt.Sprintf("%s: %s", path, msg)
		}
		return detail // fallback to default
	}
}

func formatterExample() {
	// Chinese error messages
	vZH := schemix.MustNew(`{
		name:     string
		age:      int & >=0 & <=150
		currency: "CNY" | "USD" | "EUR"
	}`, schemix.WithErrorFormatter(i18nFormatter("zh-CN")))

	r := vZH.ProcessWithMode(map[string]any{
		"age": int64(200), "currency": "JPY",
	}, schemix.FailAll)

	fmt.Println("  Chinese error messages:")
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s\n", e.Code, e.Message)
	}

	// English error messages — same schema, different formatter
	vEN := schemix.MustNew(`{
		name:     string
		age:      int & >=0 & <=150
		currency: "CNY" | "USD" | "EUR"
	}`, schemix.WithErrorFormatter(i18nFormatter("en")))

	r = vEN.ProcessWithMode(map[string]any{
		"age": int64(200), "currency": "JPY",
	}, schemix.FailAll)

	fmt.Println("  English error messages:")
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s\n", e.Code, e.Message)
	}

	// No formatter — default behavior (raw CUE/blob messages)
	vDefault := schemix.MustNew(`{
		name:     string
		age:      int & >=0 & <=150
	}`)

	r = vDefault.Process(map[string]any{"age": int64(200)})
	fmt.Println("  Default (no formatter):")
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s\n", e.Code, e.Message)
	}
}
