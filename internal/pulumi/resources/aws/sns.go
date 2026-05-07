package aws

import (
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

const (
	snsServiceCode = "AmazonSNS"
	snsService     = "SNS"
)

// snsEntry builds a DecodedResource for an SNS pricing query.
func snsEntry(record resources.ResourceRecord, region, inputsJSON, subLabel string, attrs, props map[string]string) resources.DecodedResource {
	a := map[string]string{"servicecode": snsServiceCode}
	for k, v := range attrs {
		a[k] = v
	}
	return resources.DecodedResource{
		Provider:   "aws",
		Region:     region,
		Service:    snsService,
		Name:       record.Name,
		SubLabel:   subLabel,
		RawType:    record.Type,
		Attrs:      a,
		Props:      props,
		InputsJSON: inputsJSON,
	}
}

// snsBaseProps builds the common display props for an SNS topic.
func snsBaseProps(record resources.ResourceRecord) map[string]string {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "name"); v != "" {
		props["name"] = v
	}
	if v := ExtractInput(record.Inputs, "fifoTopic"); v != "" {
		props["fifoTopic"] = v
	}
	if v := ExtractInput(record.Inputs, "tracingConfig"); v != "" {
		props["tracingConfig"] = v
	}
	return props
}

// DecodeSNSTopic splits an SNS Topic into multiple pricing queries:
//
//   - API Requests (always) — Standard uses SNS-Requests-Tier1, FIFO uses SNS-FIFO-Requests
//   - FIFO Storage (when fifoTopic=true) — SNS-FIFO-Storage
//   - FIFO Archive Processing (when fifoTopic=true and archivePolicy is set) — SNS-FIFO-ArchiveProcessing
//   - Standard Payload Message Filtering Matched (when filterPolicy is set and fifoTopic=false) — SNS-Standard-Payload-MessageFiltering-Filter-Matched
//   - Standard Payload Message Filtering Filtered-Out (when filterPolicy is set and fifoTopic=false) — SNS-Standard-Payload-MessageFiltering-Filtered-Out
//   - FIFO Payload Message Filtering Matched (when filterPolicy is set and fifoTopic=true) — SNS-FIFO-Payload-MessageFiltering-Filter-Matched
//   - FIFO Payload Message Filtering Filtered-Out (when filterPolicy is set and fifoTopic=true) — SNS-FIFO-Payload-MessageFiltering-Filtered-Out
//
// Message Delivery pricing is handled separately by DecodeSNSTopicSubscription,
// since the endpoint type is determined by the subscription's protocol.
func DecodeSNSTopic(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	isFIFO := strings.EqualFold(ExtractInput(record.Inputs, "fifoTopic"), "true")
	hasFilterPolicy := ExtractInput(record.Inputs, "filterPolicy") != ""
	hasArchivePolicy := ExtractInput(record.Inputs, "archivePolicy") != ""

	props := snsBaseProps(record)

	var results []resources.DecodedResource

	// 1. API Requests — every topic incurs publish request charges.
	requestGroup := "SNS-Requests-Tier1"
	if isFIFO {
		requestGroup = "SNS-FIFO-Requests"
	}
	results = append(results, snsEntry(record, region, inputsJSON, "Requests",
		map[string]string{"group": requestGroup, "productFamily": "API Request"},
		props))

	// 2. FIFO-specific charges.
	if isFIFO {
		// FIFO Storage: messages stored in the FIFO topic queue.
		results = append(results, snsEntry(record, region, inputsJSON, "FIFO Storage",
			map[string]string{"group": "SNS-FIFO-Storage"},
			props))

		// FIFO Archive Processing: charged when archivePolicy is configured.
		if hasArchivePolicy {
			results = append(results, snsEntry(record, region, inputsJSON, "FIFO Archive Processing",
				map[string]string{"group": "SNS-FIFO-ArchiveProcessing"},
				props))
		}

		// FIFO Payload Message Filtering (when filterPolicy is set).
		if hasFilterPolicy {
			results = append(results, snsEntry(record, region, inputsJSON, "FIFO Filter Matched",
				map[string]string{"group": "SNS-FIFO-Payload-MessageFiltering-Filter-Matched"},
				props))
			results = append(results, snsEntry(record, region, inputsJSON, "FIFO Filter Filtered-Out",
				map[string]string{"group": "SNS-FIFO-Payload-MessageFiltering-Filtered-Out"},
				props))
		}
	} else {
		// Standard Payload Message Filtering (when filterPolicy is set).
		if hasFilterPolicy {
			results = append(results, snsEntry(record, region, inputsJSON, "Filter Matched",
				map[string]string{"group": "SNS-Standard-Payload-MessageFiltering-Filter-Matched"},
				props))
			results = append(results, snsEntry(record, region, inputsJSON, "Filter Filtered-Out",
				map[string]string{"group": "SNS-Standard-Payload-MessageFiltering-Filtered-Out"},
				props))
		}
	}

	return results
}

// protocolToEndpointType maps a Pulumi SNS subscription protocol to the AWS
// pricing endpointType attribute used in the "Message Delivery" product family.
func protocolToEndpointType(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "sqs":
		return "Amazon SQS"
	case "lambda":
		return "AWS Lambda"
	case "http", "https":
		return "HTTP"
	case "sms":
		return "SMS"
	case "firehose":
		return "Amazon Kinesis Data Firehose"
	case "email", "email-json":
		return "SMTP"
	case "application":
		// Mobile push — default to APNS; actual platform depends on PlatformApplication.
		return "Apple Push Notification Service (APNS)"
	default:
		return ""
	}
}

// DecodeSNSTopicSubscription maps an SNS TopicSubscription to a Message Delivery
// pricing query. The endpoint type is derived from the subscription's protocol.
// Returns nil when the protocol cannot be mapped to a known endpoint type.
func DecodeSNSTopicSubscription(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	protocol := ExtractInput(record.Inputs, "protocol")
	endpointType := protocolToEndpointType(protocol)

	props := map[string]string{"type": record.Type}
	if protocol != "" {
		props["protocol"] = protocol
	}
	if v := ExtractInput(record.Inputs, "topic"); v != "" {
		props["topic"] = v
	}
	if endpointType != "" {
		props["endpointType"] = endpointType
	}

	if endpointType == "" {
		// Unknown protocol — no pricing available.
		return []resources.DecodedResource{{
			Provider:   "aws",
			Region:     region,
			Service:    snsService,
			Name:       record.Name,
			RawType:    record.Type,
			NoPricing:  true,
			Props:      props,
			InputsJSON: inputsJSON,
		}}
	}

	return []resources.DecodedResource{
		snsEntry(record, region, inputsJSON, "Delivery",
			map[string]string{
				"productFamily": "Message Delivery",
				"endpointType":  endpointType,
			},
			props),
	}
}
