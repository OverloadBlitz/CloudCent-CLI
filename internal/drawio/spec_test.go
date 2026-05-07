package drawio

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
)

// testMeta returns a minimal MetadataResponse with DrawioResources populated.
// Keys use the full normalised shape key format (e.g. "mxgraph.aws4.ec2_instance").
func testMeta() *api.MetadataResponse {
	return &api.MetadataResponse{
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
			"mxgraph.aws4.rds_instance": {
				Provider: "aws",
				Product:  "rds",
				Attrs: map[string]api.DrawioAttrMapping{
					"instance_type":     {Default: ""},
					"db_engine":         {Default: ""},
					"deployment_option": {Default: "Single-AZ"},
					"storage_gb":        {Default: ""},
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
				"instance_type": {"t3.micro", "t3.small", "m5.large"},
				"os_app_bundle": {"linux", "windows"},
				"tenancy":       {"Shared", "Dedicated"},
			},
		},
		ProductRegions: map[string][]string{
			"ec2": {"us-east-1", "us-west-2"},
		},
	}
}

func TestToDecodedEC2(t *testing.T) {
	comp := SpecComponent{
		ID:       "n1",
		Label:    "web-1",
		Service:  "ec2_instance",
		ShapeKey: "mxgraph.aws4.ec2_instance",
		Region:   "us-west-2",
		Attrs: map[string]string{
			"instance_type": "t3.micro",
			"os_app_bundle": "linux",
			"tenancy":       "Shared",
		},
	}

	defaults := SpecDefaults{Provider: "aws", Region: "us-east-1"}
	got, err := comp.ToDecoded(defaults, testMeta())
	if err != nil {
		t.Fatalf("ToDecoded returned error: %v", err)
	}

	if got.Provider != "aws" {
		t.Errorf("provider = %q, want aws", got.Provider)
	}
	if got.Region != "us-west-2" {
		t.Errorf("region = %q, want us-west-2 (component override)", got.Region)
	}
	if got.Service != "ec2" {
		t.Errorf("service = %q, want ec2", got.Service)
	}
	if got.Attrs["instance_type"] != "t3.micro" {
		t.Errorf("expected instance_type t3.micro, got %q", got.Attrs["instance_type"])
	}
	if got.Attrs["tenancy"] != "Shared" {
		t.Errorf("expected tenancy Shared, got %q", got.Attrs["tenancy"])
	}
}

func TestToDecodedDefaultsFromDef(t *testing.T) {
	// tenancy and os_app_bundle have defaults in the def — they should be
	// applied even when the user doesn't supply them in the spec.
	comp := SpecComponent{
		ID:       "n2",
		Label:    "web-2",
		Service:  "ec2_instance",
		ShapeKey: "mxgraph.aws4.ec2_instance",
		Attrs: map[string]string{
			"instance_type": "t3.small",
			// os_app_bundle and tenancy intentionally omitted — should use def defaults
		},
	}
	got, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, testMeta())
	if err != nil {
		t.Fatalf("ToDecoded: %v", err)
	}
	if got.Attrs["os_app_bundle"] != "linux" {
		t.Errorf("expected os_app_bundle default linux, got %q", got.Attrs["os_app_bundle"])
	}
	if got.Attrs["tenancy"] != "Shared" {
		t.Errorf("expected tenancy default Shared, got %q", got.Attrs["tenancy"])
	}
}

func TestToDecodedFallsBackToDefaultRegion(t *testing.T) {
	comp := SpecComponent{
		ID:       "n3",
		Label:    "web-3",
		Service:  "ec2_instance",
		ShapeKey: "mxgraph.aws4.ec2_instance",
		Attrs: map[string]string{
			"instance_type": "t3.small",
		},
	}
	got, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, testMeta())
	if err != nil {
		t.Fatalf("ToDecoded: %v", err)
	}
	if got.Region != "us-east-1" {
		t.Errorf("region = %q, want us-east-1 (default)", got.Region)
	}
}

func TestToDecodedMissingRequiredAttrs(t *testing.T) {
	comp := SpecComponent{
		ID:       "n4",
		Label:    "db-1",
		Service:  "rds_instance",
		ShapeKey: "mxgraph.aws4.rds_instance",
		Attrs: map[string]string{
			"db_engine": "mysql",
			// instance_type and storage_gb intentionally missing, no defaults in def
		},
	}
	_, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, testMeta())
	if err == nil {
		t.Fatal("expected error for missing RDS attrs, got nil")
	}
	if !strings.Contains(err.Error(), "instance_type") {
		t.Errorf("error should mention missing instance_type: %v", err)
	}
}

func TestToDecodedNoPricingWhenNotInMetadata(t *testing.T) {
	comp := SpecComponent{
		ID:       "v1",
		Label:    "main vpc",
		Service:  "virtual_private_cloud",
		ShapeKey: "mxgraph.aws4.virtual_private_cloud",
	}
	got, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, testMeta())
	if err != nil {
		t.Fatalf("ToDecoded: %v", err)
	}
	if !got.NoPricing {
		t.Errorf("VPC should be NoPricing (not in DrawioResources)")
	}
}

func TestToDecodedNoPricingWhenMetaNil(t *testing.T) {
	comp := SpecComponent{
		ID:       "n5",
		Label:    "web",
		Service:  "ec2_instance",
		ShapeKey: "mxgraph.aws4.ec2_instance",
		Attrs:    map[string]string{"instance_type": "t3.micro"},
	}
	got, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, nil)
	if err != nil {
		t.Fatalf("ToDecoded: %v", err)
	}
	// Without metadata, can't resolve the def → NoPricing.
	if !got.NoPricing {
		t.Errorf("expected NoPricing when meta is nil")
	}
}

func TestToDecodedS3(t *testing.T) {
	comp := SpecComponent{
		ID:       "b1",
		Label:    "media",
		Service:  "s3",
		ShapeKey: "mxgraph.aws4.s3",
		Attrs: map[string]string{
			"storage_gb": "100",
			// storage_class omitted — should use default "Standard"
		},
	}
	got, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, testMeta())
	if err != nil {
		t.Fatalf("ToDecoded: %v", err)
	}
	if got.Service != "s3" {
		t.Errorf("service = %q, want s3", got.Service)
	}
	if got.Attrs["storage_class"] != "Standard" {
		t.Errorf("storage_class = %q, want Standard (from default)", got.Attrs["storage_class"])
	}
	if got.Attrs["storage_gb"] != "100" {
		t.Errorf("storage_gb = %q, want 100", got.Attrs["storage_gb"])
	}
}

func TestToDecodedValueMap(t *testing.T) {
	meta := &api.MetadataResponse{
		DrawioResources: map[string]api.DrawioResourceDef{
			"mxgraph.aws4.rds_instance": {
				Provider: "aws",
				Product:  "rds",
				Attrs: map[string]api.DrawioAttrMapping{
					"db_engine": {
						Default: "",
						Map:     map[string]string{"postgres": "PostgreSQL", "mysql": "MySQL"},
					},
				},
			},
		},
	}
	comp := SpecComponent{
		ID:       "db1",
		Label:    "db",
		Service:  "rds_instance",
		ShapeKey: "mxgraph.aws4.rds_instance",
		Attrs:    map[string]string{"db_engine": "postgres"},
	}
	got, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, meta)
	if err != nil {
		t.Fatalf("ToDecoded: %v", err)
	}
	if got.Attrs["db_engine"] != "PostgreSQL" {
		t.Errorf("expected value map to translate postgres→PostgreSQL, got %q", got.Attrs["db_engine"])
	}
}

func TestToDecodedPulumiTypeInheritance(t *testing.T) {
	// DrawioResourceDef with pulumi_type — attrs should be inherited from
	// the pulumi def, with drawio def attrs overlaying them.
	meta := &api.MetadataResponse{
		DrawioResources: map[string]api.DrawioResourceDef{
			"mxgraph.aws4.ec2_instance": {
				Provider:   "aws",
				Product:    "ec2",
				PulumiType: "aws:ec2/instance:Instance",
				Attrs: map[string]api.DrawioAttrMapping{
					// drawio overrides tenancy default to "Shared"
					"tenancy": {Default: "Shared"},
				},
			},
		},
		PulumiResources: map[string]api.PulumiResourceDef{
			"aws:ec2/instance:Instance": {
				Provider: "aws",
				Product:  "ec2",
				Attrs: map[string]api.PulumiAttrMapping{
					"instanceType":    {Default: ""},
					"operatingSystem": {Default: "Linux"},
					"tenancy":         {Default: "dedicated"}, // pulumi default — drawio should override
				},
			},
		},
	}
	comp := SpecComponent{
		ID:       "i1",
		Label:    "web",
		Service:  "ec2_instance",
		ShapeKey: "mxgraph.aws4.ec2_instance",
		Attrs: map[string]string{
			"instanceType": "t3.micro",
			// operatingSystem uses pulumi default "Linux"
			// tenancy uses drawio override "Shared" (not pulumi's "dedicated")
		},
	}
	got, err := comp.ToDecoded(SpecDefaults{Provider: "aws", Region: "us-east-1"}, meta)
	if err != nil {
		t.Fatalf("ToDecoded: %v", err)
	}
	if got.Attrs["instanceType"] != "t3.micro" {
		t.Errorf("instanceType = %q, want t3.micro", got.Attrs["instanceType"])
	}
	if got.Attrs["operatingSystem"] != "Linux" {
		t.Errorf("operatingSystem = %q, want Linux (inherited from pulumi def)", got.Attrs["operatingSystem"])
	}
	if got.Attrs["tenancy"] != "Shared" {
		t.Errorf("tenancy = %q, want Shared (drawio override wins over pulumi default)", got.Attrs["tenancy"])
	}
}

func TestSpecRoundTrip(t *testing.T) {
	meta := testMeta()
	d := &Diagram{
		Components: []Component{
			{ID: "a", Label: "web", ServiceType: "ec2_instance", ShapeKey: "mxgraph.aws4.ec2_instance", Provider: "aws"},
			{ID: "b", Label: "db", ServiceType: "rds_instance", ShapeKey: "mxgraph.aws4.rds_instance", Provider: "aws"},
			{ID: "c", Label: "vpc", ServiceType: "virtual_private_cloud", ShapeKey: "mxgraph.aws4.virtual_private_cloud", Provider: "aws"},
		},
	}
	original := GenerateSpec(d, meta, "us-east-1")

	var buf bytes.Buffer
	if err := WriteSpec(&buf, original, meta); err != nil {
		t.Fatalf("WriteSpec: %v", err)
	}

	parsed, err := ParseSpec(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}

	if parsed.Version != original.Version {
		t.Errorf("version mismatch: %d vs %d", parsed.Version, original.Version)
	}
	if len(parsed.Components) != len(original.Components) {
		t.Fatalf("component count mismatch: %d vs %d", len(parsed.Components), len(original.Components))
	}
	for i, comp := range parsed.Components {
		if comp.ID != original.Components[i].ID {
			t.Errorf("component[%d].ID = %q, want %q", i, comp.ID, original.Components[i].ID)
		}
		if comp.Service != original.Components[i].Service {
			t.Errorf("component[%d].Service = %q, want %q", i, comp.Service, original.Components[i].Service)
		}
		if comp.ShapeKey != original.Components[i].ShapeKey {
			t.Errorf("component[%d].ShapeKey = %q, want %q", i, comp.ShapeKey, original.Components[i].ShapeKey)
		}
		if comp.NoPricing != original.Components[i].NoPricing {
			t.Errorf("component[%d].NoPricing = %v, want %v", i, comp.NoPricing, original.Components[i].NoPricing)
		}
	}
}

func TestDefaultSpecPath(t *testing.T) {
	cases := map[string]string{
		"arch.drawio":        "arch.cloudcent.yaml",
		"path/to/foo.drawio": "path/to/foo.cloudcent.yaml",
		"noext":              "noext.cloudcent.yaml",
		"/abs/path/x.drawio": "/abs/path/x.cloudcent.yaml",
	}
	for in, want := range cases {
		if got := DefaultSpecPath(in); got != want {
			t.Errorf("DefaultSpecPath(%q) = %q, want %q", in, got, want)
		}
	}
}
