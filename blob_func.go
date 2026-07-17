package schemix

import (
	"time"

	"github.com/redpanda-data/benthos/v4/public/bloblang"
)

// builtinFunctions returns all built-in validation functions to register.
func builtinFunctions() []struct {
	name string
	spec *bloblang.PluginSpec
	ctor bloblang.FunctionConstructorV2
} {
	return []struct {
		name string
		spec *bloblang.PluginSpec
		ctor bloblang.FunctionConstructorV2
	}{
		// --- Comparison functions ---
		{
			name: "in_list",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Returns true if the first argument is found in the remaining arguments").
				Param(bloblang.NewAnyParam("value").Description("value to search for")).
				Param(bloblang.NewAnyParam("candidates").Description("list of allowed values")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Function, error) {
				// in_list receives: value, candidates...
				// Bloblang variadic isn't supported in V2, so we use 2 params:
				// value (the thing to check) and candidates (an array)
				raw, err := args.Get("value")
				if err != nil {
					return nil, err
				}
				candidates, err := args.Get("candidates")
				if err != nil {
					return nil, err
				}
				return func() (any, error) {
					list, ok := candidates.([]any)
					if !ok {
						// Single value comparison
						return raw == candidates, nil
					}
					for _, item := range list {
						if raw == item {
							return true, nil
						}
					}
					return false, nil
				}, nil
			},
		},

		// --- Date/time functions ---
		{
			name: "is_past_date",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Returns true if the date string (RFC3339 or 2006-01-02) is in the past").
				Param(bloblang.NewStringParam("date").Description("date string to validate")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Function, error) {
				dateStr, err := args.GetString("date")
				if err != nil {
					return nil, err
				}
				return func() (any, error) {
					t, parseErr := parseDate(dateStr)
					if parseErr != nil {
						return false, nil
					}
					return t.Before(time.Now()), nil
				}, nil
			},
		},
		{
			name: "is_future_date",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Returns true if the date string (RFC3339 or 2006-01-02) is in the future").
				Param(bloblang.NewStringParam("date").Description("date string to validate")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Function, error) {
				dateStr, err := args.GetString("date")
				if err != nil {
					return nil, err
				}
				return func() (any, error) {
					t, parseErr := parseDate(dateStr)
					if parseErr != nil {
						return false, nil
					}
					return t.After(time.Now()), nil
				}, nil
			},
		},
		{
			name: "is_valid_date",
			spec: bloblang.NewPluginSpec().Category(categoryValidation).
				Description("Returns true if the string is a valid date (tries RFC3339, then 2006-01-02)").
				Param(bloblang.NewStringParam("date").Description("date string to validate")),
			ctor: func(args *bloblang.ParsedParams) (bloblang.Function, error) {
				dateStr, err := args.GetString("date")
				if err != nil {
					return nil, err
				}
				return func() (any, error) {
					_, parseErr := parseDate(dateStr)
					return parseErr == nil, nil
				}, nil
			},
		},
	}
}

// parseDate tries common date formats.
func parseDate(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01-02 15:04:05",
		"2006/01/02",
		"02-01-2006",
		"01/02/2006",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, &time.ParseError{}
}
