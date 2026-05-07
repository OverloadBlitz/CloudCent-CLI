package drawio

import (
	"fmt"
	"testing"
)

// validCompressedInput is a draw.io <mxfile> with a compressed <diagram>.
// Encoding pipeline: encodeURIComponent(xml) → raw deflate → base64
// Contains: EC2 Instance (vertex 2), S3 Bucket (vertex 3), edge 2→3.
const validCompressedInput = `<mxfile host="Electron"><diagram id="test1" name="Page-1">xVPBjsIgEP0argbBZD3bdY0HT/sFhE6gkZYGptr+/dJOXYOuiSYmeyCZeW9mePMCTBZ1vwuqtQdfgmNyy2QRvEeK6r4A55jgVcnkJxOCp8PE1wN2ObG8VQEafKZBUMNJuQ4I2RYjsG8iqkYD0REHN9PRqnYM696MohfqHFeLANF3QcNe+4bJTUopyqtAX26DgNA/VDxBs9wd+BowDKnk0sBpIz7c5OeqREvYx5ogC5WxmGMqUm5+J1/tScHs0N9uyTu3vhPEN50+Ar7Tqijf4JT8R6dWNAJKA5l6Wj57e6iCAcwMfmLHAE5hdcqnv6A4pddfNnHZJ/wB</diagram></mxfile>`

func TestDecodeCompressed(t *testing.T) {
	d, err := Parse(validCompressedInput)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("Components: %d\n", len(d.Components))
	for _, c := range d.Components {
		fmt.Printf("  [%s] %q (type=%s)\n", c.ID, c.Label, c.ServiceType)
	}
	fmt.Printf("Edges: %d\n", len(d.Edges))

	if len(d.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(d.Components))
	}
	if len(d.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(d.Edges))
	}
}

func TestDecodePlainXML(t *testing.T) {
	plain := `<mxGraphModel><root>
		<mxCell id="0"/>
		<mxCell id="1" parent="0"/>
		<mxCell id="2" value="Lambda" style="shape=mxgraph.aws4.lambda" vertex="1" parent="1">
			<mxGeometry x="10" y="10" width="78" height="78" as="geometry"/>
		</mxCell>
	</root></mxGraphModel>`

	d, err := Parse(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Components) != 1 {
		t.Errorf("expected 1 component, got %d", len(d.Components))
	}
	if d.Components[0].Label != "Lambda" {
		t.Errorf("expected label 'Lambda', got %q", d.Components[0].Label)
	}
}

func TestDetectServiceFromShape(t *testing.T) {
	cases := map[string]struct {
		label        string
		style        string
		wantService  string
		wantProvider string
		wantShapeKey string
	}{
		"aws4 resIcon wins over generic shape": {
			label:        "Amazon S3",
			style:        "sketch=0;outlineConnect=0;fontColor=#232F3E;gradientColor=none;fillColor=#E7157B;strokeColor=#fff;dashed=0;verticalLabelPosition=bottom;verticalAlign=top;align=center;html=1;fontSize=12;fontStyle=0;aspect=fixed;shape=mxgraph.aws4.resourceIcon;resIcon=mxgraph.aws4.s3;",
			wantService:  "s3",
			wantProvider: "aws",
			wantShapeKey: "mxgraph.aws4.s3",
		},
		"aws4 plain shape suffix": {
			label:        "EIP",
			style:        "shape=mxgraph.aws4.elastic_ip_address;fillColor=#E7157B;strokeColor=#fff;",
			wantService:  "elastic_ip_address",
			wantProvider: "aws",
			wantShapeKey: "mxgraph.aws4.elastic_ip_address",
		},
		"aws3 maps to aws provider": {
			label:        "S3 (legacy stencil)",
			style:        "shape=mxgraph.aws3.s3;",
			wantService:  "s3",
			wantProvider: "aws",
			wantShapeKey: "mxgraph.aws3.s3",
		},
		"azure stencil maps to azure provider": {
			label:        "VM",
			style:        "shape=mxgraph.azure.virtual_machine;fillColor=#0078D7;",
			wantService:  "virtual_machine",
			wantProvider: "azure",
			wantShapeKey: "mxgraph.azure.virtual_machine",
		},
		"gcp stencil maps to gcp provider": {
			label:        "GCE",
			style:        "shape=mxgraph.gcp.compute_engine;",
			wantService:  "compute_engine",
			wantProvider: "gcp",
			wantShapeKey: "mxgraph.gcp.compute_engine",
		},
		"oci stencil maps to oci provider": {
			label:        "OCI Compute",
			style:        "shape=mxgraph.oci.compute;",
			wantService:  "compute",
			wantProvider: "oci",
			wantShapeKey: "mxgraph.oci.compute",
		},
		"grIcon group shape": {
			label:        "Auto Scaling group",
			style:        "points=[[0,0]];outlineConnect=0;gradientColor=none;html=1;whiteSpace=wrap;fontSize=12;fontStyle=0;container=1;pointerEvents=0;collapsible=0;recursiveResize=0;shape=mxgraph.aws4.group;grIcon=mxgraph.aws4.group_auto_scaling_group;",
			wantService:  "group_auto_scaling_group",
			wantProvider: "aws",
			wantShapeKey: "mxgraph.aws4.group_auto_scaling_group",
		},
		"prIcon productIcon wrapper": {
			label:        "Elastic Load Balancing",
			style:        "outlineConnect=0;fontColor=#D05C17;gradientColor=#F78E04;strokeColor=#ffffff;fillColor=#D05C17;dashed=0;verticalLabelPosition=middle;verticalAlign=middle;align=left;html=1;whiteSpace=wrap;fontSize=16;fontStyle=0;shape=mxgraph.aws4.productIcon;prIcon=mxgraph.aws4.elastic_load_balancing;",
			wantService:  "elastic_load_balancing",
			wantProvider: "aws",
			wantShapeKey: "mxgraph.aws4.elastic_load_balancing",
		},
		"label is ignored when style is empty": {
			label:        "EC2 Instance",
			style:        "",
			wantService:  "",
			wantProvider: "",
			wantShapeKey: "",
		},
		"label is ignored even when it names a service": {
			label:        "Amazon S3 Bucket",
			style:        "",
			wantService:  "",
			wantProvider: "",
			wantShapeKey: "",
		},
		"unknown stencil prefix returns lowercased suffix and empty provider": {
			label:        "custom",
			style:        "shape=stencil(myCustomShape);",
			wantService:  "stencil(mycustomshape)",
			wantProvider: "",
			wantShapeKey: "stencil(mycustomshape)",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotService, gotProvider, gotShapeKey := DetectService(tc.label, tc.style)
			if gotService != tc.wantService {
				t.Errorf("DetectService(%q, %q) service = %q, want %q", tc.label, tc.style, gotService, tc.wantService)
			}
			if gotProvider != tc.wantProvider {
				t.Errorf("DetectService(%q, %q) provider = %q, want %q", tc.label, tc.style, gotProvider, tc.wantProvider)
			}
			if gotShapeKey != tc.wantShapeKey {
				t.Errorf("DetectService(%q, %q) shapeKey = %q, want %q", tc.label, tc.style, gotShapeKey, tc.wantShapeKey)
			}
		})
	}
}
