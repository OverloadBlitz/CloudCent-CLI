package aws

import (
	"fmt"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// DecodeLogGroup splits a CloudWatch Log Group into:
//   - Ingestion (Data Payload / PutLogEvents)
//   - Storage (Storage Snapshot) — skipped for DELIVERY class (fixed 2-day retention)
func DecodeLogGroup(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	logGroupClass := ExtractInput(record.Inputs, "logGroupClass")

	group := "Ingested Logs"
	if strings.EqualFold(logGroupClass, "INFREQUENT_ACCESS") {
		group = "IA Custom ingested Logs"
	}

	props := map[string]string{"type": record.Type}
	if logGroupClass != "" {
		props["logGroupClass"] = logGroupClass
	}
	if v := ExtractInput(record.Inputs, "name"); v != "" {
		props["name"] = v
	}
	if v := ExtractInput(record.Inputs, "retentionInDays"); v != "" {
		props["retentionInDays"] = v
	}

	result := []resources.DecodedResource{
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
		result = append(result, resources.DecodedResource{
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

	return result
}

// DecodeContributorInsightRule decodes a CloudWatch Contributor Insight Rule.
// Produces two billable resources: the rule itself and matched events.
// Parses ruleDefinition JSON to determine if it targets CloudWatch Logs or DynamoDB.
func DecodeContributorInsightRule(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "ruleName"); v != "" {
		props["ruleName"] = v
	}

	// Determine rule type from ruleDefinition JSON.
	// If it contains "LogGroupNames" → CloudWatch Logs, otherwise assume DynamoDB.
	operation := "CloudWatchLogs"
	eventGroup := "Event-CloudWatchLog"
	ruleDef := ExtractInput(record.Inputs, "ruleDefinition")
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

// DecodeInternetMonitor decodes a CloudWatch Internet Monitor.
// Produces two billable resources: monitored resources and city-networks.
func DecodeInternetMonitor(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "monitorName"); v != "" {
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

// AddMetricAlarmType determines the alarm type from inputs and writes
// alarmType into both attrs (for API matching) and props (for display).
// Types: Standard, High Resolution, Metrics Insights.
func AddMetricAlarmType(record resources.ResourceRecord, attrs, props map[string]string) {
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
		period := ExtractInput(record.Inputs, "period")
		if period != "" {
			p := 0
			fmt.Sscanf(period, "%d", &p)
			if p > 0 && p < 60 {
				alarmType = "High Resolution"
			}
		}
	}

	// Write alarmType into attrs so the pricing API can match on it.
	attrs["alarmType"] = alarmType
	props["alarmType"] = alarmType
}

// AddLogSubscriptionFilterDestination infers the logsDestination attr from
// the destinationArn input.
func AddLogSubscriptionFilterDestination(record resources.ResourceRecord, attrs, props map[string]string) {
	destArn := ExtractInput(record.Inputs, "destinationArn")
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
