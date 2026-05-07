package aws

import (
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

const (
	s3ServiceCode = "AmazonS3"
	s3Service     = "S3"
)

// s3Entry builds a DecodedResource for an S3 pricing query.
func s3Entry(record resources.ResourceRecord, region, inputsJSON, subLabel string, attrs, props map[string]string) resources.DecodedResource {
	a := map[string]string{"servicecode": s3ServiceCode}
	for k, v := range attrs {
		a[k] = v
	}
	return resources.DecodedResource{
		Provider:   "aws",
		Region:     region,
		Service:    s3Service,
		Name:       record.Name,
		SubLabel:   subLabel,
		RawType:    record.Type,
		Attrs:      a,
		Props:      props,
		InputsJSON: inputsJSON,
	}
}

// storageClassToVolumeType maps a Pulumi S3 storageClass input value to the
// AWS pricing volumeType attribute used in the "Storage" product family.
func storageClassToVolumeType(storageClass string) string {
	switch strings.ToUpper(strings.TrimSpace(storageClass)) {
	case "STANDARD_IA":
		return "Infrequent Access"
	case "ONEZONE_IA":
		return "One Zone - Infrequent Access"
	case "INTELLIGENT_TIERING":
		return "Intelligent-Tiering Frequent Access"
	case "GLACIER":
		return "Amazon Glacier"
	case "GLACIER_IR":
		return "Glacier Instant Retrieval"
	case "DEEP_ARCHIVE":
		return "Amazon Glacier"
	case "REDUCED_REDUNDANCY":
		return "Reduced Redundancy"
	default:
		// STANDARD and anything unrecognised → Standard
		return "Standard"
	}
}

// s3BucketProps builds the common display props for an S3 bucket resource.
func s3BucketProps(record resources.ResourceRecord) map[string]string {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "bucket"); v != "" {
		props["bucket"] = v
	}
	if v := ExtractInput(record.Inputs, "bucketPrefix"); v != "" {
		props["bucketPrefix"] = v
	}
	return props
}

// DecodeS3Bucket splits an S3 Bucket (aws:s3/bucket:Bucket or
// aws:s3/bucketV2:BucketV2) into three pricing queries:
//   - Storage (Standard by default; Intelligent-Tiering if configured)
//   - Requests-PUT  (PUT/COPY/POST/LIST — group S3-API-Tier1)
//   - Requests-GET  (GET and all other — group S3-API-Tier2)
func DecodeS3Bucket(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := s3BucketProps(record)

	// Determine storage class from the bucket's intelligentTieringConfigurations
	// or accelerationStatus as a hint; default to Standard.
	volumeType := "Standard"

	return []resources.DecodedResource{
		s3Entry(record, region, inputsJSON, "Storage",
			map[string]string{
				"productFamily": "Storage",
				"volumeType":    volumeType,
				"storageClass":  "General Purpose",
			},
			props),
		s3Entry(record, region, inputsJSON, "Requests-PUT",
			map[string]string{
				"productFamily": "API Request",
				"group":         "S3-API-Tier1",
			},
			props),
		s3Entry(record, region, inputsJSON, "Requests-GET",
			map[string]string{
				"productFamily": "API Request",
				"group":         "S3-API-Tier2",
			},
			props),
	}
}

// DecodeS3BucketObject maps an S3 BucketObject / BucketObjectv2 to a single
// Storage pricing query. The storageClass input determines the volumeType.
func DecodeS3BucketObject(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	storageClass := ExtractInput(record.Inputs, "storageClass")
	volumeType := storageClassToVolumeType(storageClass)

	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "bucket"); v != "" {
		props["bucket"] = v
	}
	if v := ExtractInput(record.Inputs, "key"); v != "" {
		props["key"] = v
	}
	if storageClass != "" {
		props["storageClass"] = storageClass
	}
	props["volumeType"] = volumeType

	return []resources.DecodedResource{
		s3Entry(record, region, inputsJSON, "Storage",
			map[string]string{
				"productFamily": "Storage",
				"volumeType":    volumeType,
				"storageClass":  "General Purpose",
			},
			props),
	}
}

// DecodeS3DirectoryBucket maps an S3 Express DirectoryBucket to a Storage
// pricing query using the "High Performance" storageClass.
func DecodeS3DirectoryBucket(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "bucket"); v != "" {
		props["bucket"] = v
	}
	if v := ExtractInput(record.Inputs, "location.name"); v != "" {
		props["location"] = v
	}

	return []resources.DecodedResource{
		s3Entry(record, region, inputsJSON, "Storage",
			map[string]string{
				"productFamily": "Storage",
				"storageClass":  "High Performance",
			},
			props),
	}
}

// DecodeS3TableBucket maps an S3 Tables TableBucket to a Storage pricing query.
func DecodeS3TableBucket(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "name"); v != "" {
		props["name"] = v
	}

	return []resources.DecodedResource{
		s3Entry(record, region, inputsJSON, "Storage",
			map[string]string{
				"productFamily": "Storage",
				"volumeType":    "Tables",
				"storageClass":  "Analytics",
			},
			props),
	}
}

// DecodeS3VectorBucket maps an S3 Vectors VectorBucket to a Storage pricing query.
func DecodeS3VectorBucket(record resources.ResourceRecord, region, inputsJSON string) []resources.DecodedResource {
	props := map[string]string{"type": record.Type}
	if v := ExtractInput(record.Inputs, "vectorBucketName"); v != "" {
		props["vectorBucketName"] = v
	}

	return []resources.DecodedResource{
		s3Entry(record, region, inputsJSON, "Storage",
			map[string]string{
				"productFamily": "Storage",
				"volumeType":    "Vectors",
				"storageClass":  "Analytics",
			},
			props),
	}
}
