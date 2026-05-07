package aws

import (
	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

// DecodeEventArchive produces two billable resources: archived events and storage.
func DecodeEventArchive(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "name"); v != "" {
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

// AddEventRuleInvocationType sets the invocation attr to "Scheduled Invocation"
// when the rule uses a scheduleExpression, leaving it unset for event-pattern rules.
func AddEventRuleInvocationType(record resources.ResourceRecord, attrs, props map[string]string) {
	scheduleExpr := ExtractInput(record.Inputs, "scheduleExpression")
	if scheduleExpr != "" {
		attrs["invocation"] = "Scheduled Invocation"
		props["invocation"] = "Scheduled Invocation"
		props["scheduleExpression"] = scheduleExpr
		return
	}

	eventPattern := ExtractInput(record.Inputs, "eventPattern")
	if eventPattern != "" {
		props["eventPattern"] = eventPattern
	}
}
