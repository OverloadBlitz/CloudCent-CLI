package pulumi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// skipTypes are internal Pulumi resource types that are never user-visible resources.
var skipTypes = map[string]bool{
	"pulumi:pulumi:Stack":    true,
	"pulumi:providers:aws":   true,
	"pulumi:providers:azure": true,
	"pulumi:providers:gcp":   true,
	"pulumi:providers:oci":   true,
}

// azureVersionRe strips the version segment from azure-native resource types.
// e.g. "azure-native:compute/v20240301:VirtualMachine" → "azure-native:compute:VirtualMachine"
var azureVersionRe = regexp.MustCompile(`/v20\d{6}[^:]*`)

// normalizeType strips Azure version segments so versioned types match the
// unversioned entries in the metadata map.
func normalizeType(typ string) string {
	if strings.HasPrefix(typ, "azure-native:") {
		return azureVersionRe.ReplaceAllString(typ, "")
	}
	return typ
}

// DecodeAllResources decodes collected Pulumi resources using the metadata-driven
// mapping. It replaces the old per-resource decoder approach — no Go code changes
// are needed to support new resource types; just update pulumi_resource_map.json
// in the pipeline and refresh metadata.
func DecodeAllResources(records []resources.ResourceRecord, meta *api.MetadataResponse) []resources.DecodedResource {
	// Build free-type lookup set from metadata.
	freeSet := make(map[string]bool, len(meta.FreeTypes))
	for _, ft := range meta.FreeTypes {
		freeSet[ft] = true
	}

	var results []resources.DecodedResource

	for _, record := range records {
		if skipTypes[record.Type] {
			continue
		}

		normalizedType := normalizeType(record.Type)

		// Check free types first.
		if freeSet[record.Type] || freeSet[normalizedType] {
			results = append(results, decodeFreeResource(record))
			continue
		}

		// Look up in pulumi_resources mapping.
		mapping, ok := meta.PulumiResources[normalizedType]
		if !ok {
			// Also try the original type (in case normalizeType changed it).
			mapping, ok = meta.PulumiResources[record.Type]
		}
		if !ok {
			// Unknown type — include as no-pricing so it still shows in output.
			results = append(results, resources.DecodedResource{
				Name:       record.Name,
				RawType:    record.Type,
				NoPricing:  true,
				Props:      inputsToProps(record),
				InputsJSON: formatInputProperties(record.Inputs),
			})
			continue
		}

		decoded := decodeFromMapping(record, mapping)
		results = append(results, decoded...)
	}

	return results
}

// decodeFromMapping uses a metadata PulumiResourceDef to extract pricing
// attributes from a Pulumi resource's inputs. Returns one or more
// DecodedResources — most resource types produce one, but some (e.g. Lambda)
// are split into multiple pricing queries.
func decodeFromMapping(record resources.ResourceRecord, mapping api.PulumiResourceDef) []resources.DecodedResource {
	attrs := make(map[string]string)
	props := map[string]string{"type": record.Type}

	for canonicalName, attrDef := range mapping.Attrs {
		val := ""

		if attrDef.Input != "" {
			val = extractInput(record.Inputs, attrDef.Input)
		}

		if val == "" && attrDef.Default != "" {
			val = attrDef.Default
		}
		if val != "" && len(attrDef.Map) > 0 {
			val = applyValueMap(val, attrDef.Map)
		}

		if val != "" {
			attrs[canonicalName] = val
			props[canonicalName] = val
		}
	}

	// Region comes from the collector's MockedProperties.
	region := ""
	if record.MockedProperties != nil {
		region = record.MockedProperties["region"]
	}

	t := normalizeType(record.Type)
	inputsJSON := formatInputProperties(record.Inputs)

	// Resources that produce multiple pricing queries (1:N).
	switch t {
	case "aws:lambda/function:Function":
		return decodeLambda(record, mapping, region, props, inputsJSON)
	case "aws:cloudwatch/logGroup:LogGroup":
		return decodeLogGroup(record, region, inputsJSON)
	case "aws:cloudwatch/contributorInsightRule:ContributorInsightRule":
		return decodeContributorInsightRule(record, region, inputsJSON)
	case "aws:cloudwatch/internetMonitor:InternetMonitor":
		return decodeInternetMonitor(record, region, inputsJSON)
	case "aws:cloudwatch/eventArchive:EventArchive":
		return decodeEventArchive(record, region, inputsJSON)
		//case "aws:dynamodb/table:Table":
		//	return decodeDynamodb()
	}

	// single request
	addDerivedAttrs(record, mapping, attrs, props)

	return []resources.DecodedResource{{
		Provider:   mapping.Provider,
		Region:     region,
		Service:    mapping.Product,
		Name:       record.Name,
		RawType:    record.Type,
		Attrs:      attrs,
		Props:      props,
		InputsJSON: inputsJSON,
	}}
}

func mockedOS(record resources.ResourceRecord) string {
	if record.MockedProperties == nil {
		return ""
	}
	os := strings.ToLower(strings.TrimSpace(record.MockedProperties["os"]))
	switch os {
	case "linux", "windows":
		return os
	default:
		return ""
	}
}

func addDerivedAttrs(record resources.ResourceRecord, mapping api.PulumiResourceDef, attrs, props map[string]string) {
	t := normalizeType(record.Type)

	switch t {
	case "aws:ec2/instance:Instance":
		addMockedOS(record, mapping, attrs, props)
	case "aws:apigatewayv2/api:Api":
		addAPIGatewayV2ProtocolType(record, attrs, props)
	case "aws:cloudwatch/metricAlarm:MetricAlarm":
		addMetricAlarmType(record, attrs, props)
	case "aws:cloudwatch/logSubscriptionFilter:LogSubscriptionFilter":
		addLogSubscriptionFilterDestination(record, attrs, props)
	case "aws:cloudwatch/eventRule:EventRule":
		addEventRuleInvocationType(record, attrs, props)
	}
}

// addMockedOS overrides the operatingSystem attr with the collector-inferred
// value from MockedProperties. If the collector didn't resolve an OS, the
// attr is removed so the default from the mapping doesn't leak through.
func addMockedOS(record resources.ResourceRecord, mapping api.PulumiResourceDef, attrs, props map[string]string) {
	if _, defined := mapping.Attrs["operatingSystem"]; !defined {
		return
	}
	if val := mockedOS(record); val != "" {
		attrs["operatingSystem"] = val
		props["operatingSystem"] = val
	} else {
		delete(attrs, "operatingSystem")
		delete(props, "operatingSystem")
	}
}

func addAPIGatewayV2ProtocolType(record resources.ResourceRecord, attrs, props map[string]string) {

	protocolType := extractInput(record.Inputs, "protocolType")
	if protocolType == "WEBSOCKET" {
		attrs["productFamily"] = "WebSocket"
		props["productFamily"] = "WebSocket"
		attrs["operation"] = "ApiGatewayWebSocket"
		props["operation"] = "ApiGatewayWebSocket"
	}
}

// addMetricAlarmType determines the alarm type from inputs and overrides
// the group attr accordingly: Standard, High Resolution, or Metrics Insights.
func addMetricAlarmType(record resources.ResourceRecord, attrs, props map[string]string) {
	alarmType := "Standard"

	// metricQueries is an array — check PropertyMap directly.
	mqVal, hasMQ := record.Inputs[resource.PropertyKey("metricQueries")]
	if hasMQ && mqVal.IsArray() && len(mqVal.ArrayValue()) > 0 {
		for _, item := range mqVal.ArrayValue() {
			if item.IsObject() {
				if expr, ok := item.ObjectValue()[resource.PropertyKey("expression")]; ok && expr.IsString() {
					if strings.Contains(strings.ToUpper(expr.StringValue()), "SELECT") {
						attrs["group"] = "Metrics Insights Alarms"
						props["group"] = "Metrics Insights Alarms"
						alarmType = "Metrics Insights"
						break
					}
				}
			}
		}
	} else {
		period := extractInput(record.Inputs, "period")
		if period != "" {
			p := 0
			fmt.Sscanf(period, "%d", &p)
			if p > 0 && p < 60 {
				alarmType = "High Resolution"
			}
		}
	}

	props["alarmType"] = alarmType
}

// addLogSubscriptionFilterDestination infers the logsDestination attr from
// the destinationArn input.
func addLogSubscriptionFilterDestination(record resources.ResourceRecord, attrs, props map[string]string) {
	destArn := extractInput(record.Inputs, "destinationArn")
	if destArn == "" {
		return
	}
	props["destinationArn"] = destArn

	if strings.Contains(destArn, ":firehose:") {
		attrs["logsDestination"] = "Amazon Kinesis Data Firehose"
	} else if strings.Contains(destArn, ":s3:") || strings.Contains(destArn, ":s3:::") {
		attrs["logsDestination"] = "Amazon S3"
	} else if strings.Contains(destArn, ":logs:") {
		attrs["logsDestination"] = "Amazon CloudWatch Logs"
	}
}

// addEventRuleInvocationType sets the invocation attr to "Scheduled Invocation"
// when the rule uses a scheduleExpression, leaving it unset for event-pattern rules.
func addEventRuleInvocationType(record resources.ResourceRecord, attrs, props map[string]string) {
	scheduleExpr := extractInput(record.Inputs, "scheduleExpression")
	if scheduleExpr != "" {
		attrs["invocation"] = "Scheduled Invocation"
		props["invocation"] = "Scheduled Invocation"
		props["scheduleExpression"] = scheduleExpr
		return
	}

	eventPattern := extractInput(record.Inputs, "eventPattern")
	if eventPattern != "" {
		props["eventPattern"] = eventPattern
	}
}

// decodeLambda splits a Lambda function into multiple pricing
// queries based on the function's properties:
//   - Requests (always)
//   - Duration (always)
//   - Ephemeral Storage Duration (when ephemeralStorage.size > 512 MB)
//
// Architecture (x86 vs ARM) selects the appropriate group variant.
func decodeLambda(record resources.ResourceRecord, mapping api.PulumiResourceDef, region string, _ map[string]string, inputsJSON string) []resources.DecodedResource {
	arch := strings.ToLower(strings.TrimSpace(extractInput(record.Inputs, "architectures")))
	isArm := arch == "arm64"

	armSuffix := ""
	if isArm {
		armSuffix = "-ARM"
	}

	// Build clean props for Lambda — don't inherit attrs from the generic
	// metadata loop (e.g. instanceType mapped from memorySize is misleading).
	lambdaProps := map[string]string{"type": record.Type}
	if arch != "" {
		lambdaProps["architecture"] = arch
	}
	if v := extractInput(record.Inputs, "memorySize"); v != "" {
		lambdaProps["memorySize"] = v + " MB"
	}
	if v := extractInput(record.Inputs, "timeout"); v != "" {
		lambdaProps["timeout"] = v + "s"
	}
	if v := extractInput(record.Inputs, "runtime"); v != "" {
		lambdaProps["runtime"] = v
	}
	if v := extractInput(record.Inputs, "ephemeralStorage.size"); v != "" {
		lambdaProps["ephemeralStorage"] = v + " MB"
	}

	base := func(subLabel, group string) resources.DecodedResource {
		return resources.DecodedResource{
			Provider:   mapping.Provider,
			Region:     region,
			Service:    mapping.Product,
			Name:       record.Name,
			SubLabel:   subLabel,
			RawType:    record.Type,
			Attrs:      map[string]string{"group": group, "servicecode": "AWSLambda"},
			Props:      lambdaProps,
			InputsJSON: inputsJSON,
		}
	}

	results := []resources.DecodedResource{
		base("Requests", "AWS-Lambda-Requests"+armSuffix),
		base("Duration", "AWS-Lambda-Duration"+armSuffix),
	}

	// Ephemeral storage beyond the free 512 MB incurs additional charges.
	ephemeralSize := extractInput(record.Inputs, "ephemeralStorage.size")
	if ephemeralSize != "" {
		size := 0
		fmt.Sscanf(ephemeralSize, "%d", &size)
		if size > 512 {
			results = append(results, base("Ephemeral Storage", "AWS-Lambda-Storage-Duration"+armSuffix))
		}
	}

	return results
}

//func decodeDynamodb(record resources.ResourceRecord, mapping api.PulumiResourceDef, region string, _ map[string]string, inputsJSON string) []resources.DecodedResource {
//
//}

// applyValueMap translates a raw value using the provided map.
// Lookup is case-insensitive; if no mapping matches the original value is returned.
func applyValueMap(val string, m map[string]string) string {
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

// extractInput reads a value from a Pulumi PropertyMap, supporting dot-path
// notation for nested objects (e.g. "hardwareProfile.vmSize", "sku.name").
func extractInput(inputs resource.PropertyMap, path string) string {
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
			return propertyToString(val)
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

// propertyToString converts a Pulumi PropertyValue to a string.
func propertyToString(v resource.PropertyValue) string {
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

// decodeFreeResource creates a no-pricing DecodedResource for free resource types.
func decodeFreeResource(record resources.ResourceRecord) resources.DecodedResource {
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
		InputsJSON: formatInputProperties(record.Inputs),
	}
}

func formatInputProperties(inputs resource.PropertyMap) string {
	if len(inputs) == 0 {
		return ""
	}

	data, err := json.MarshalIndent(propertyMapToAny(inputs), "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", inputs)
	}
	return string(data)
}

func propertyMapToAny(pm resource.PropertyMap) map[string]any {
	out := make(map[string]any, len(pm))
	for k, v := range pm {
		out[string(k)] = propertyValueToAny(v)
	}
	return out
}

func propertyValueToAny(v resource.PropertyValue) any {
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
			out = append(out, propertyValueToAny(item))
		}
		return out
	case v.IsObject():
		return propertyMapToAny(v.ObjectValue())
	case v.IsNull():
		return nil
	default:
		return fmt.Sprintf("%v", v)
	}
}

// inputsToProps converts a resource's inputs to a flat string map for display.
func inputsToProps(record resources.ResourceRecord) map[string]string {
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

// decodeLogGroup splits a CloudWatch Log Group into:
//   - Ingestion (Data Payload / PutLogEvents)
//   - Storage (Storage Snapshot) — skipped for DELIVERY class (fixed 2-day retention)
func decodeLogGroup(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	logGroupClass := extractInput(record.Inputs, "logGroupClass")

	group := "Ingested Logs"
	if strings.EqualFold(logGroupClass, "INFREQUENT_ACCESS") {
		group = "IA Custom ingested Logs"
	}

	props := map[string]string{"type": record.Type}
	if logGroupClass != "" {
		props["logGroupClass"] = logGroupClass
	}
	if v := extractInput(record.Inputs, "name"); v != "" {
		props["name"] = v
	}
	if v := extractInput(record.Inputs, "retentionInDays"); v != "" {
		props["retentionInDays"] = v
	}

	results := []resources.DecodedResource{
		{
			Provider:   "aws",
			Region:     region,
			Service:    "CloudWatch Logs",
			Name:       record.Name,
			SubLabel:   "Ingestion",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "Data Payload", "operation": "PutLogEvents", "group": group, "servicecode": "AmazonCloudWatch"},
			Props:      props,
			InputsJSON: inputsJSON,
		},
	}

	// DELIVERY class has fixed 2-day retention with no separate storage charge.
	if !strings.EqualFold(logGroupClass, "DELIVERY") {
		results = append(results, resources.DecodedResource{
			Provider:   "aws",
			Region:     region,
			Service:    "CloudWatch Logs",
			Name:       record.Name,
			SubLabel:   "Storage",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "Storage Snapshot", "servicecode": "AmazonCloudWatch"},
			Props:      props,
			InputsJSON: inputsJSON,
		})
	}

	return results
}

// decodeContributorInsightRuledecodes a CloudWatch Contributor Insight Rule.
// Produces two billable resources: the rule itself and matched events.
// Parses ruleDefinition JSON to determine if it targets CloudWatch Logs or DynamoDB.
func decodeContributorInsightRule(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := extractInput(record.Inputs, "ruleName"); v != "" {
		props["ruleName"] = v
	}

	// Determine rule type from ruleDefinition JSON.
	// If it contains "LogGroupNames" → CloudWatch Logs, otherwise assume DynamoDB.
	operation := "CloudWatchLogs"
	eventGroup := "Event-CloudWatchLog"
	ruleDef := extractInput(record.Inputs, "ruleDefinition")
	if ruleDef != "" {
		if strings.Contains(ruleDef, "DynamoDB") || !strings.Contains(ruleDef, "LogGroupNames") {
			operation = "DynamoDB"
			eventGroup = "Event-DynamoDB"
		}
	}

	return []resources.DecodedResource{
		{
			Provider:   "aws",
			Region:     region,
			Service:    "CloudWatch Contributor Insights",
			Name:       record.Name,
			SubLabel:   "Rule",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "Contributor Insights", "operation": operation, "servicecode": "AmazonCloudWatch"},
			Props:      props,
			InputsJSON: inputsJSON,
		},
		{
			Provider:   "aws",
			Region:     region,
			Service:    "CloudWatch Contributor Insights",
			Name:       record.Name,
			SubLabel:   "Matched Events",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "N/A", "group": eventGroup, "servicecode": "AmazonCloudWatch"},
			Props:      props,
			InputsJSON: inputsJSON,
		},
	}
}

// decodeInternetMonitor decodes a CloudWatch Internet Monitor.
// Produces two billable resources: monitored resources and city-networks.
func decodeInternetMonitor(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := extractInput(record.Inputs, "monitorName"); v != "" {
		props["monitorName"] = v
	}

	return []resources.DecodedResource{
		{
			Provider:   "aws",
			Region:     region,
			Service:    "CloudWatch Internet Monitor",
			Name:       record.Name,
			SubLabel:   "Monitored Resources",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "N/A", "group": "CW-InternetMonitor", "operation": "putMonitoredResource", "servicecode": "AmazonCloudWatch"},
			Props:      props,
			InputsJSON: inputsJSON,
		},
		{
			Provider:   "aws",
			Region:     region,
			Service:    "CloudWatch Internet Monitor",
			Name:       record.Name,
			SubLabel:   "City Networks",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "N/A", "group": "CW-CityNetwork", "operation": "putCityNetwork", "servicecode": "AmazonCloudWatch"},
			Props:      props,
			InputsJSON: inputsJSON,
		},
	}
}

// decodeEventArchive Produces two billable resources: archived events and storage.
func decodeEventArchive(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := extractInput(record.Inputs, "name"); v != "" {
		props["name"] = v
	}

	return []resources.DecodedResource{
		{
			Provider:   "aws",
			Region:     region,
			Service:    "EventBridge",
			Name:       record.Name,
			SubLabel:   "Archive Events",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "CloudWatch Events", "operation": "ArchiveEvents", "eventType": "Archive Event", "servicecode": "AWSEvents"},
			Props:      props,
			InputsJSON: inputsJSON,
		},
		{
			Provider:   "aws",
			Region:     region,
			Service:    "EventBridge",
			Name:       record.Name,
			SubLabel:   "Storage",
			RawType:    record.Type,
			Attrs:      map[string]string{"productFamily": "CloudWatch Events", "operation": "StandardStorage", "eventType": "Event Storage", "servicecode": "AWSEvents"},
			Props:      props,
			InputsJSON: inputsJSON,
		},
	}
}
