package aws

import (
	"fmt"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

// DecodeLambda splits a Lambda function into multiple pricing queries:
//   - Requests (always)
//   - Duration (always)
//   - Ephemeral Storage Duration (when ephemeralStorage.size > 512 MB)
//
// Architecture (x86 vs ARM) selects the appropriate group variant.
func DecodeLambda(record resources.ResourceRecord, mapping api.PulumiResourceDef, region string, _ map[string]string, inputsJSON string) []resources.DecodedResource {
	arch := strings.ToLower(strings.TrimSpace(ExtractInput(record.Inputs, "architectures")))
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
	if v := ExtractInput(record.Inputs, "memorySize"); v != "" {
		lambdaProps["memorySize"] = v + " MB"
	}
	if v := ExtractInput(record.Inputs, "timeout"); v != "" {
		lambdaProps["timeout"] = v + "s"
	}
	if v := ExtractInput(record.Inputs, "runtime"); v != "" {
		lambdaProps["runtime"] = v
	}
	if v := ExtractInput(record.Inputs, "ephemeralStorage.size"); v != "" {
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

	result := []resources.DecodedResource{
		base("Requests", "AWS-Lambda-Requests"+armSuffix),
		base("Duration", "AWS-Lambda-Duration"+armSuffix),
	}

	// Ephemeral storage beyond the free 512 MB incurs additional charges.
	ephemeralSize := ExtractInput(record.Inputs, "ephemeralStorage.size")
	if ephemeralSize != "" {
		size := 0
		fmt.Sscanf(ephemeralSize, "%d", &size)
		if size > 512 {
			result = append(result, base("Ephemeral Storage", "AWS-Lambda-Storage-Duration"+armSuffix))
		}
	}

	return result
}
