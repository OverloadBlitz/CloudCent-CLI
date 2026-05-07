package aws

import (
	"fmt"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

// volumeTypeToProductFamily maps EBS volumeApiName to the AWS pricing productFamily.
var volumeTypeToProductFamily = map[string]string{
	"gp2":      "General Purpose",
	"gp3":      "General Purpose",
	"io1":      "Provisioned IOPS",
	"io2":      "Provisioned IOPS",
	"sc1":      "Cold HDD",
	"st1":      "Throughput Optimized HDD",
	"standard": "Magnetic",
}

// DecodeEBSVolume splits an aws:ebs/volume:Volume into multiple pricing queries:
//   - Storage (always)
//   - System Operation / IOPS (io1, io2, gp3 with provisioned IOPS)
//   - Provisioned Throughput (gp3 with provisioned throughput > 125 MiB/s)
func DecodeEBSVolume(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	volType := strings.ToLower(strings.TrimSpace(ExtractInput(record.Inputs, "type")))
	if volType == "" {
		volType = "gp2" // AWS default
	}

	volumeFamily, ok := volumeTypeToProductFamily[volType]
	if !ok {
		volumeFamily = "General Purpose"
	}

	sizeStr := ExtractInput(record.Inputs, "size")
	iopsStr := ExtractInput(record.Inputs, "iops")
	throughputStr := ExtractInput(record.Inputs, "throughput")

	props := map[string]string{
		"type":         record.Type,
		"volumeType":   volType,
		"volumeFamily": volumeFamily,
	}
	if sizeStr != "" {
		props["size"] = sizeStr + " GiB"
	}
	if iopsStr != "" {
		props["iops"] = iopsStr
	}
	if throughputStr != "" {
		props["throughput"] = throughputStr + " MiB/s"
	}

	var result []resources.DecodedResource

	// ── 1. Storage ──────────────────────────────────────────────────────────
	result = append(result, resources.DecodedResource{
		Provider: "aws",
		Region:   region,
		Service:  "ec2",
		Name:     record.Name,
		SubLabel: "Storage",
		RawType:  record.Type,
		Attrs: map[string]string{
			"volumeApiName": volType,
			"volumeType":    volumeFamily,
			"storageMedia":  storageMediaForType(volType),
		},
		Props:      props,
		InputsJSON: inputsJSON,
	})

	// ── 2. IOPS charges ─────────────────────────────────────────────────────
	switch volType {
	case "io1", "io2":
		// Provisioned IOPS — always charged per IOPS-month.
		result = append(result, resources.DecodedResource{
			Provider: "aws",
			Region:   region,
			Service:  "ec2",
			Name:     record.Name,
			SubLabel: "Provisioned IOPS",
			RawType:  record.Type,
			Attrs: map[string]string{
				"volumeApiName": volType,
				"provisioned":   "Yes",
				"group":         "EBS IOPS",
			},
			Props:      props,
			InputsJSON: inputsJSON,
		})

	case "gp3":
		// gp3 baseline is 3000 IOPS free; provisioned IOPS above that are charged.
		provisionedIOPS := 0
		if iopsStr != "" {
			fmt.Sscanf(iopsStr, "%d", &provisionedIOPS)
		}
		if provisionedIOPS > 3000 {
			result = append(result, resources.DecodedResource{
				Provider: "aws",
				Region:   region,
				Service:  "ec2",
				Name:     record.Name,
				SubLabel: "Provisioned IOPS",
				RawType:  record.Type,
				Attrs: map[string]string{
					"volumeApiName": "gp3",
					"provisioned":   "Yes",
					"group":         "EBS IOPS",
				},
				Props:      props,
				InputsJSON: inputsJSON,
			})
		}

		// gp3 baseline throughput is 125 MiB/s free; above that is charged.
		provisionedThroughput := 0
		if throughputStr != "" {
			fmt.Sscanf(throughputStr, "%d", &provisionedThroughput)
		}
		if provisionedThroughput > 125 {
			result = append(result, resources.DecodedResource{
				Provider: "aws",
				Region:   region,
				Service:  "ec2",
				Name:     record.Name,
				SubLabel: "Provisioned Throughput",
				RawType:  record.Type,
				Attrs: map[string]string{
					"volumeApiName": "gp3",
					"provisioned":   "Yes",
					"group":         "EBS Throughput",
				},
				Props:      props,
				InputsJSON: inputsJSON,
			})
		}

	case "gp2", "standard":
		// gp2/standard charge per I/O request (not provisioned).
		result = append(result, resources.DecodedResource{
			Provider: "aws",
			Region:   region,
			Service:  "ec2",
			Name:     record.Name,
			SubLabel: "I/O Requests",
			RawType:  record.Type,
			Attrs: map[string]string{
				"volumeApiName": volType,
				"provisioned":   "No",
				"group":         "EBS I/O Requests",
			},
			Props:      props,
			InputsJSON: inputsJSON,
		})
	}

	return result
}

// storageMediaForType returns the AWS pricing storageMedia value for a volume type.
func storageMediaForType(volType string) string {
	switch volType {
	case "sc1", "st1":
		return "HDD-backed"
	default:
		return "SSD-backed"
	}
}
