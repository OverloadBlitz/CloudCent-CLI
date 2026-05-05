package estimate

import (
	"encoding/json"
	"testing"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

type stubBatchPricingClient struct {
	lastRequest api.BatchPricingRequest
	response    *api.BatchPricingApiResponse
	err         error
}

func (s *stubBatchPricingClient) FetchPricingBatch(req api.BatchPricingRequest) (*api.BatchPricingApiResponse, error) {
	s.lastRequest = req
	return s.response, s.err
}

func TestEstimateAllResourcesMatchesPricingItemsByResourceFields(t *testing.T) {
	resp := api.BatchPricingApiResponse{
		"opaque-response-key": {
			{
				Product:  "ec2",
				Provider: "aws",
				Region:   "us-west-2",
				Attributes: map[string]*api.AttrValue{
					"instanceType":  mustAttrValue(t, `"t3.micro"`),
					"os_app_bundle": mustAttrValue(t, `"linux"`),
				},
				Prices: []api.Price{
					{
						PricingModel: stringPtr("OnDemand"),
						Unit:         stringPtr("Hrs"),
						Rates: []api.PriceRate{
							{Price: mustAttrValue(t, `"0.0104"`)},
						},
					},
				},
				MinPrice: mustAttrValue(t, `"0.0104"`),
				MaxPrice: mustAttrValue(t, `"0.0104"`),
			},
		},
	}
	client := &stubBatchPricingClient{response: &resp}

	results, err := EstimateAllResources(client, []resources.DecodedResource{
		{
			Provider: "aws",
			Region:   "us-west-2",
			Service:  "ec2",
			Name:     "web-server",
			Attrs: map[string]string{
				"instanceType":  "t3.micro",
				"os_app_bundle": "linux",
				"tenancy":       "",
			},
			PriceFilter: ">=0.2",
		},
	}, nil)
	if err != nil {
		t.Fatalf("EstimateAllResources returned error: %v", err)
	}

	if len(client.lastRequest.Requests) != 1 {
		t.Fatalf("expected 1 batch request, got %d", len(client.lastRequest.Requests))
	}

	request := client.lastRequest.Requests[0]
	if request.Product != "ec2" {
		t.Fatalf("expected product ec2 in batch request, got %q", request.Product)
	}
	if request.Region != "us-west-2" {
		t.Fatalf("expected region us-west-2 in batch request, got %q", request.Region)
	}
	if _, ok := request.Attrs["tenancy"]; ok {
		t.Fatalf("expected empty attrs to be omitted from batch request")
	}
	if got := request.Attrs["os_app_bundle"]; got != "linux" {
		t.Fatalf("expected os_app_bundle attr to be linux, got %q", got)
	}
	if got := request.Price; got != ">=0.2" {
		t.Fatalf("expected price filter >=0.2 in batch request, got %q", got)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 estimate result, got %d", len(results))
	}

	result := results[0]
	if result.ResourceName != "web-server" {
		t.Fatalf("expected resource name web-server, got %q", result.ResourceName)
	}
	if result.Product != "aws ec2" {
		t.Fatalf("expected display product aws ec2, got %q", result.Product)
	}
	if len(result.Prices) == 0 {
		t.Fatalf("expected at least one price entry")
	}
	if result.Prices[0].Model != "OnDemand" {
		t.Fatalf("expected first price model OnDemand, got %q", result.Prices[0].Model)
	}
	if result.Prices[0].RatePerHr != 0.0104 {
		t.Fatalf("expected OnDemand rate 0.0104, got %f", result.Prices[0].RatePerHr)
	}
	if !result.Prices[0].IsCurrent {
		t.Fatalf("expected OnDemand price to be marked as current")
	}
}

func TestEstimateAllResourcesMatchesPricingItemsIgnoringCase(t *testing.T) {
	resp := api.BatchPricingApiResponse{
		"mixed-case-response": {
			{
				Product:  "EC2",
				Provider: "AWS",
				Region:   "US-EAST-2",
				Attributes: map[string]*api.AttrValue{
					"INSTANCE_TYPE": mustAttrValue(t, `"t2.micro"`),
					"OS_APP_BUNDLE": mustAttrValue(t, `"Linux"`),
					"TENANCY":       mustAttrValue(t, `"shared"`),
				},
				Prices: []api.Price{
					{
						PricingModel: stringPtr("OnDemand"),
						Unit:         stringPtr("Hrs"),
						Rates: []api.PriceRate{
							{Price: mustAttrValue(t, `"0.0116"`)},
						},
					},
				},
			},
		},
	}
	client := &stubBatchPricingClient{response: &resp}

	results, err := EstimateAllResources(client, []resources.DecodedResource{
		{
			Provider: "aws",
			Region:   "us-east-2",
			Service:  "ec2",
			Name:     "web-server-www",
			Attrs: map[string]string{
				"instance_type": "t2.micro",
				"os_app_bundle": "linux",
				"tenancy":       "Shared",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("EstimateAllResources returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 estimate result, got %d", len(results))
	}

	if results[0].OnDemandRate != 0.0116 {
		t.Fatalf("expected mixed-case response to match OnDemand rate 0.0116, got %f", results[0].OnDemandRate)
	}
}

func TestEstimateAllResourcesReusesSamePricingItemForDuplicateResources(t *testing.T) {
	resp := api.BatchPricingApiResponse{
		"shared-price": {
			{
				Product:  "ec2",
				Provider: "aws",
				Attributes: map[string]*api.AttrValue{
					"instanceType":  mustAttrValue(t, `"t3.micro"`),
					"os_app_bundle": mustAttrValue(t, `"linux"`),
				},
				Prices: []api.Price{
					{
						PricingModel: stringPtr("OnDemand"),
						Unit:         stringPtr("Hrs"),
						Rates: []api.PriceRate{
							{Price: mustAttrValue(t, `"0.0104"`)},
						},
					},
				},
			},
		},
	}
	client := &stubBatchPricingClient{response: &resp}

	results, err := EstimateAllResources(client, []resources.DecodedResource{
		{
			Provider: "aws",
			Service:  "ec2",
			Name:     "web-1",
			Attrs: map[string]string{
				"instanceType":  "t3.micro",
				"os_app_bundle": "linux",
			},
		},
		{
			Provider: "aws",
			Service:  "ec2",
			Name:     "web-2",
			Attrs: map[string]string{
				"instanceType":  "t3.micro",
				"os_app_bundle": "linux",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("EstimateAllResources returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 estimate results, got %d", len(results))
	}

	for _, result := range results {
		if result.OnDemandRate != 0.0104 {
			t.Fatalf("expected duplicated resources to share the same OnDemand rate 0.0104, got %f", result.OnDemandRate)
		}
	}
}

func mustAttrValue(t *testing.T, raw string) *api.AttrValue {
	t.Helper()

	var value api.AttrValue
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("failed to build AttrValue from %s: %v", raw, err)
	}

	return &value
}

func stringPtr(v string) *string {
	return &v
}
