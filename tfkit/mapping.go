package tfkit

import (
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The Set* helpers project a framework model value onto a JSON request body,
// skipping null and unknown values so an unset attribute never appears on the
// wire. Generated resource glue calls them to build create/update bodies.

// SetString sets body[key] from v when v is a known, non-null string.
func SetString(body map[string]any, key string, v types.String) {
	if !v.IsNull() && !v.IsUnknown() {
		body[key] = v.ValueString()
	}
}

// SetInt64 sets body[key] from v when v is a known, non-null integer.
func SetInt64(body map[string]any, key string, v types.Int64) {
	if !v.IsNull() && !v.IsUnknown() {
		body[key] = v.ValueInt64()
	}
}

// SetBool sets body[key] from v when v is a known, non-null boolean.
func SetBool(body map[string]any, key string, v types.Bool) {
	if !v.IsNull() && !v.IsUnknown() {
		body[key] = v.ValueBool()
	}
}

// SetFloat64 sets body[key] from v when v is a known, non-null number.
func SetFloat64(body map[string]any, key string, v types.Float64) {
	if !v.IsNull() && !v.IsUnknown() {
		body[key] = v.ValueFloat64()
	}
}

// The Get* helpers read a decoded JSON value back into a framework type,
// yielding a null value when the key is absent. They tolerate JSON's numeric
// decoding (numbers arrive as float64).

// GetString reads data[key] as a string, or null when absent/mistyped.
func GetString(data map[string]any, key string) types.String {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return types.StringValue(s)
		}
	}
	return types.StringNull()
}

// GetInt64 reads data[key] as an integer, or null when absent/mistyped.
func GetInt64(data map[string]any, key string) types.Int64 {
	switch n := data[key].(type) {
	case float64:
		return types.Int64Value(int64(n))
	case int64:
		return types.Int64Value(n)
	case int:
		return types.Int64Value(int64(n))
	}
	return types.Int64Null()
}

// GetBool reads data[key] as a boolean, or null when absent/mistyped.
func GetBool(data map[string]any, key string) types.Bool {
	if b, ok := data[key].(bool); ok {
		return types.BoolValue(b)
	}
	return types.BoolNull()
}

// GetFloat64 reads data[key] as a number, or null when absent/mistyped.
func GetFloat64(data map[string]any, key string) types.Float64 {
	if f, ok := data[key].(float64); ok {
		return types.Float64Value(f)
	}
	return types.Float64Null()
}

// MaskKeys returns the keys of a request body sorted, for use as an AIP-134
// updateMask. Deterministic ordering keeps generated update calls stable.
func MaskKeys(body map[string]any) []string {
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
