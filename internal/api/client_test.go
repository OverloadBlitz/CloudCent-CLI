package api

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OverloadBlitz/cloudcent-cli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestClient(t *testing.T, withAuth bool, fn roundTripFunc) *Client {
	t.Helper()

	client := &Client{
		http: &http.Client{Transport: fn},
	}
	if withAuth {
		apiKey := "test-api-key"
		client.Config = &config.Config{
			CliID:  "test-cli-id",
			APIKey: &apiKey,
		}
	}
	return client
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestPostIncludesJSONAndAuthHeaders(t *testing.T) {
	client := newTestClient(t, true, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", req.Method)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected JSON content type, got %q", got)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("expected auth header, got %q", got)
		}
		if got := req.Header.Get("X-Cli-Id"); got != "test-cli-id" {
			t.Fatalf("expected cli id header, got %q", got)
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if got := string(body); got != `{"exchange_code":"abc123"}` {
			t.Fatalf("unexpected request body: %s", got)
		}

		return response(http.StatusOK, `{"status":"ok"}`), nil
	})

	status, data, err := client.post("https://example.com/test", map[string]string{"exchange_code": "abc123"}, true)
	if err != nil {
		t.Fatalf("post returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if string(data) != `{"status":"ok"}` {
		t.Fatalf("unexpected response body: %s", string(data))
	}
}

func TestFetchPricingReturnsEmptyResponseOnNotFound(t *testing.T) {
	client := newTestClient(t, true, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", req.Method)
		}
		if req.URL.Path != "/pricing" {
			t.Fatalf("expected /pricing path, got %s", req.URL.Path)
		}

		query := req.URL.Query()
		if got := query["provider"]; len(got) != 1 || got[0] != "aws" {
			t.Fatalf("unexpected provider query: %#v", got)
		}
		if got := query["products"]; len(got) != 1 || got[0] != "ec2" {
			t.Fatalf("unexpected products query: %#v", got)
		}
		if got := query["region"]; len(got) != 1 || got[0] != "us-east-1" {
			t.Fatalf("unexpected region query: %#v", got)
		}

		return response(http.StatusNotFound, "not found"), nil
	})

	result, err := client.FetchPricing(
		[]string{"aws ec2"},
		[]string{"us-east-1"},
		map[string]string{"instance_type": "t3.micro"},
		[]string{"onDemand"},
	)
	if err != nil {
		t.Fatalf("FetchPricing returned error: %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("expected total 0, got %d", result.Total)
	}
	if len(result.Data) != 0 {
		t.Fatalf("expected no data, got %d items", len(result.Data))
	}
}

func TestFetchPricingBatchUsesExpectedRequestShape(t *testing.T) {
	client := newTestClient(t, true, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", req.Method)
		}
		if req.URL.String() != APIBaseURL+"/pricing/batch" {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		expected := `{"requests":[{"provider":"aws","region":"us-east-1","product":"ec2","attrs":{"operatingSystem":"linux"},"price":"\u003e=0.1"},{"provider":"aws","region":"us-west-2","product":"ec2","attrs":{"operatingSystem":"linux"},"price":"\u003e=0.2"}]}`
		if string(body) != expected {
			t.Fatalf("unexpected batch request body: %s", string(body))
		}

		return response(http.StatusOK, `{}`), nil
	})

	_, err := client.FetchPricingBatch(BatchPricingRequest{
		Requests: []BatchPricingRequestItem{
			{
				Provider: "aws",
				Region:   "us-east-1",
				Product:  "ec2",
				Attrs: map[string]string{
					"operatingSystem": "linux",
				},
				Price: ">=0.1",
			},
			{
				Provider: "aws",
				Region:   "us-west-2",
				Product:  "ec2",
				Attrs: map[string]string{
					"operatingSystem": "linux",
				},
				Price: ">=0.2",
			},
		},
	})
	if err != nil {
		t.Fatalf("FetchPricingBatch returned error: %v", err)
	}
}

func TestGenerateTokenUsesPostWithoutAuthAndParsesFallbackFields(t *testing.T) {
	client := newTestClient(t, false, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", req.Method)
		}
		if req.URL.String() != CLIBaseURL+"/api/auth/generate-token" {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected no auth header, got %q", got)
		}
		if got := req.Header.Get("Content-Type"); got != "" {
			t.Fatalf("expected empty content type for nil POST body, got %q", got)
		}

		return response(http.StatusOK, `{"token":"access-token","exchange_id":"exchange-code"}`), nil
	})

	result, err := client.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if result.AccessToken != "access-token" {
		t.Fatalf("unexpected access token: %q", result.AccessToken)
	}
	if result.ExchangeCode != "exchange-code" {
		t.Fatalf("unexpected exchange code: %q", result.ExchangeCode)
	}
}

func TestDownloadMetadataGzUsesGeneralGetAndWritesFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	client := newTestClient(t, true, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET request, got %s", req.Method)
		}
		if req.URL.String() != APIBaseURL+"/pricing/metadata" {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("expected auth header, got %q", got)
		}

		return response(http.StatusOK, "gzip-content"), nil
	})

	if err := client.DownloadMetadataGz(); err != nil {
		t.Fatalf("DownloadMetadataGz returned error: %v", err)
	}

	filePath := filepath.Join(homeDir, ".cloudcent", "metadata.json.gz")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read metadata file: %v", err)
	}
	if string(data) != "gzip-content" {
		t.Fatalf("unexpected metadata file contents: %s", string(data))
	}
}

func TestGetCanSkipAuthHeaders(t *testing.T) {
	client := newTestClient(t, false, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET request, got %s", req.Method)
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected no auth header, got %q", got)
		}
		if got := req.Header.Get("X-Cli-Id"); got != "" {
			t.Fatalf("expected no cli id header, got %q", got)
		}

		return response(http.StatusOK, "ok"), nil
	})

	status, data, err := client.get((&url.URL{
		Scheme: "https",
		Host:   "example.com",
		Path:   "/health",
	}).String(), false)
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if string(data) != "ok" {
		t.Fatalf("unexpected response body: %s", string(data))
	}
}
