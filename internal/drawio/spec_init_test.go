package drawio

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
)

func TestGenerateSpecFromDrawioResources(t *testing.T) {
	meta := &api.MetadataResponse{
		DrawioResources: map[string]api.DrawioResourceDef{
			"mxgraph.aws4.ec2_instance": {
				Provider: "aws",
				Product:  "ec2",
				Attrs: map[string]api.DrawioAttrMapping{
					"instance_type": {Default: ""},
					"os_app_bundle": {Default: "linux"},
					"tenancy":       {Default: "Shared"},
				},
			},
			"mxgraph.aws4.s3": {
				Provider: "aws",
				Product:  "s3",
				Attrs: map[string]api.DrawioAttrMapping{
					"storage_class": {Default: "Standard"},
					"storage_gb":    {Default: ""},
				},
			},
		},
		AttributeValues: map[string]map[string][]string{
			"ec2": {
				"instance_type": {"t3.micro", "t3.small"},
				"os_app_bundle": {"linux", "windows"},
			},
		},
		ProductRegions: map[string][]string{
			"ec2": {"us-east-1", "us-west-2"},
			"s3":  {"us-east-1"},
		},
	}

	d := &Diagram{
		Components: []Component{
			{ID: "1", Label: "web", ServiceType: "ec2_instance", ShapeKey: "mxgraph.aws4.ec2_instance", Provider: "aws"},
			{ID: "2", Label: "static-files", ServiceType: "s3", ShapeKey: "mxgraph.aws4.s3", Provider: "aws"},
			{ID: "3", Label: "main vpc", ServiceType: "virtual_private_cloud", ShapeKey: "mxgraph.aws4.virtual_private_cloud", Provider: "aws"},
			{ID: "4", Label: "mystery", ServiceType: "", ShapeKey: ""},
		},
	}

	spec := GenerateSpec(d, meta, "us-east-1")

	if spec.Defaults.Region != "us-east-1" {
		t.Errorf("default region = %q", spec.Defaults.Region)
	}
	if len(spec.Components) != 4 {
		t.Fatalf("expected 4 components, got %d", len(spec.Components))
	}

	ec2 := spec.Components[0]
	if ec2.NoPricing {
		t.Error("EC2 should be billable")
	}
	if ec2.ShapeKey != "mxgraph.aws4.ec2_instance" {
		t.Errorf("EC2 ShapeKey = %q, want mxgraph.aws4.ec2_instance", ec2.ShapeKey)
	}
	if _, ok := ec2.Attrs["instance_type"]; !ok {
		t.Error("EC2 spec should expose instance_type attr from DrawioResources")
	}
	if ec2.Attrs["os_app_bundle"] != "linux" {
		t.Errorf("EC2 os_app_bundle should be pre-filled with default 'linux', got %q", ec2.Attrs["os_app_bundle"])
	}
	if ec2.Attrs["tenancy"] != "Shared" {
		t.Errorf("EC2 tenancy should be pre-filled with default 'Shared', got %q", ec2.Attrs["tenancy"])
	}

	s3 := spec.Components[1]
	if s3.NoPricing {
		t.Error("S3 should be billable")
	}
	if s3.Attrs["storage_class"] != "Standard" {
		t.Errorf("S3 storage_class should be pre-filled with default 'Standard', got %q", s3.Attrs["storage_class"])
	}

	vpc := spec.Components[2]
	if !vpc.NoPricing {
		t.Error("VPC should be NoPricing (not in DrawioResources)")
	}

	unknown := spec.Components[3]
	if !unknown.NoPricing {
		t.Error("Unknown service should be NoPricing")
	}
}

func TestGenerateSpecWithoutMetadata(t *testing.T) {
	// Without metadata, all components should be NoPricing.
	d := &Diagram{
		Components: []Component{
			{ID: "1", Label: "web", ServiceType: "ec2_instance", ShapeKey: "mxgraph.aws4.ec2_instance"},
			{ID: "2", Label: "vpc", ServiceType: "virtual_private_cloud", ShapeKey: "mxgraph.aws4.virtual_private_cloud"},
		},
	}

	spec := GenerateSpec(d, nil, "us-east-1")

	if len(spec.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(spec.Components))
	}
	for _, comp := range spec.Components {
		if !comp.NoPricing {
			t.Errorf("component %q should be NoPricing without metadata", comp.Service)
		}
	}
}

func TestWriteSpecEmitsExampleComments(t *testing.T) {
	meta := &api.MetadataResponse{
		DrawioResources: map[string]api.DrawioResourceDef{
			"mxgraph.aws4.ec2_instance": {
				Provider: "aws",
				Product:  "ec2",
				Attrs: map[string]api.DrawioAttrMapping{
					"instance_type": {Default: ""},
					"os_app_bundle": {Default: "linux"},
				},
			},
		},
		AttributeValues: map[string]map[string][]string{
			"ec2": {
				"instance_type": {"t3.micro", "t3.small", "m5.large"},
				"os_app_bundle": {"linux", "windows"},
			},
		},
		ProductRegions: map[string][]string{},
	}

	d := &Diagram{
		Components: []Component{
			{ID: "1", Label: "web", ServiceType: "ec2_instance", ShapeKey: "mxgraph.aws4.ec2_instance", Provider: "aws"},
		},
	}
	spec := GenerateSpec(d, meta, "us-east-1")

	var buf bytes.Buffer
	if err := WriteSpec(&buf, spec, meta); err != nil {
		t.Fatalf("WriteSpec: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "examples: t3.micro") {
		t.Errorf("expected instance_type examples in YAML, got:\n%s", out)
	}
	if !strings.Contains(out, "examples: linux, windows") {
		t.Errorf("expected os example values in YAML, got:\n%s", out)
	}
	if !strings.Contains(out, "os_app_bundle: linux") {
		t.Errorf("expected os_app_bundle pre-filled with default, got:\n%s", out)
	}
}

func TestWriteSpecOmitsCommentsWithoutMetadata(t *testing.T) {
	d := &Diagram{
		Components: []Component{
			{ID: "1", Label: "web", ServiceType: "ec2_instance", ShapeKey: "mxgraph.aws4.ec2_instance"},
		},
	}
	spec := GenerateSpec(d, nil, "us-east-1")

	var buf bytes.Buffer
	if err := WriteSpec(&buf, spec, nil); err != nil {
		t.Fatalf("WriteSpec: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "examples:") {
		t.Errorf("expected no example comments without metadata, got:\n%s", out)
	}
	// EC2 should still appear, but as noPricing since no metadata.
	if !strings.Contains(out, "service: ec2_instance") {
		t.Errorf("expected ec2_instance service entry, got:\n%s", out)
	}
	if !strings.Contains(out, "noPricing: true") {
		t.Errorf("expected noPricing: true without metadata, got:\n%s", out)
	}
}
