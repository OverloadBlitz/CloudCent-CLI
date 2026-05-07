package aws

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// ExtractInput reads a value from a Pulumi PropertyMap, supporting dot-path
// notation for nested objects (e.g. "hardwareProfile.vmSize", "sku.name").
func ExtractInput(inputs resource.PropertyMap, path string) string {
	parts := strings.Split(path, ".")
	current := inputs

	for i, part := range parts {
		key := resource.PropertyKey(part)
		val, ok := current[key]
		if !ok {
			return ""
		}

		// Last segment — extract the scalar value.
		if i == len(parts)-1 {
			return PropertyToString(val)
		}

		// Intermediate segment — must be an object to keep traversing.
		if val.IsObject() {
			current = val.ObjectValue()
		} else {
			return ""
		}
	}

	return ""
}

// PropertyToString converts a Pulumi PropertyValue to a string.
func PropertyToString(v resource.PropertyValue) string {
	if v.IsString() {
		return v.StringValue()
	}
	if v.IsNumber() {
		return fmt.Sprintf("%g", v.NumberValue())
	}
	if v.IsBool() {
		if v.BoolValue() {
			return "true"
		}
		return "false"
	}
	return ""
}

// ApplyValueMap translates a raw value using the provided map.
// Lookup is case-insensitive; if no mapping matches the original value is returned.
func ApplyValueMap(val string, m map[string]string) string {
	// Try exact match first.
	if mapped, ok := m[val]; ok {
		return mapped
	}
	// Case-insensitive fallback.
	lower := strings.ToLower(val)
	for k, v := range m {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return val
}

// FormatInputProperties serialises a Pulumi PropertyMap to indented JSON for
// display/debugging purposes.
func FormatInputProperties(inputs resource.PropertyMap) string {
	if len(inputs) == 0 {
		return ""
	}

	data, err := json.MarshalIndent(PropertyMapToAny(inputs), "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", inputs)
	}
	return string(data)
}

// PropertyMapToAny converts a Pulumi PropertyMap to a plain map[string]any.
func PropertyMapToAny(pm resource.PropertyMap) map[string]any {
	out := make(map[string]any, len(pm))
	for k, v := range pm {
		out[string(k)] = PropertyValueToAny(v)
	}
	return out
}

// PropertyValueToAny converts a Pulumi PropertyValue to a plain Go value.
func PropertyValueToAny(v resource.PropertyValue) any {
	switch {
	case v.IsString():
		return v.StringValue()
	case v.IsNumber():
		return v.NumberValue()
	case v.IsBool():
		return v.BoolValue()
	case v.IsArray():
		items := v.ArrayValue()
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, PropertyValueToAny(item))
		}
		return out
	case v.IsObject():
		return PropertyMapToAny(v.ObjectValue())
	case v.IsNull():
		return nil
	default:
		return fmt.Sprintf("%v", v)
	}
}

// InputsToProps converts a resource's inputs to a flat string map for display.
func InputsToProps(record resources.ResourceRecord) map[string]string {
	props := map[string]string{
		"type": record.Type,
	}
	for k, v := range record.Inputs {
		key := string(k)
		if v.IsString() {
			props[key] = v.StringValue()
		} else if v.IsNumber() {
			props[key] = fmt.Sprintf("%g", v.NumberValue())
		} else if v.IsBool() {
			if v.BoolValue() {
				props[key] = "true"
			} else {
				props[key] = "false"
			}
		}
	}
	return props
}

// DecodeFreeResource creates a no-pricing DecodedResource for free resource types.
func DecodeFreeResource(record resources.ResourceRecord) resources.DecodedResource {
	provider := ""
	if idx := strings.IndexByte(record.Type, ':'); idx > 0 {
		provider = record.Type[:idx]
	}
	return resources.DecodedResource{
		Provider:   provider,
		Name:       record.Name,
		RawType:    record.Type,
		NoPricing:  true,
		IsFreeType: true,
		Props:      map[string]string{"type": record.Type},
		InputsJSON: FormatInputProperties(record.Inputs),
	}
}
