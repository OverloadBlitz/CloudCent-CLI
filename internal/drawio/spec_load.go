package drawio

import (
	"fmt"
	"os"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
	"gopkg.in/yaml.v3"
)

// LoadSpec reads and parses a YAML spec file from disk.
func LoadSpec(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("reading spec %q: %w", path, err)
	}
	return ParseSpec(data)
}

// ParseSpec parses a Spec from raw YAML bytes.
func ParseSpec(data []byte) (Spec, error) {
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return Spec{}, fmt.Errorf("parsing spec yaml: %w", err)
	}
	if spec.Version == 0 {
		spec.Version = SpecVersion
	}
	return spec, nil
}

// SpecToDecoded converts every component in the spec to a
// resources.DecodedResource. Components flagged NoPricing are still returned
// (with NoPricing set) so they show up in the output as "no pricing".
//
// meta is used to validate that all required attrs for each product are
// present. Pass nil to skip validation (useful in tests without metadata).
//
// Returns a list of per-component validation errors so callers can surface
// every missing field at once instead of stopping at the first one.
func SpecToDecoded(spec Spec, meta *api.MetadataResponse) ([]resources.DecodedResource, []error) {
	out := make([]resources.DecodedResource, 0, len(spec.Components))
	var errs []error
	for _, comp := range spec.Components {
		decoded, err := comp.ToDecoded(spec.Defaults, meta)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, decoded)
	}
	return out, errs
}

// DefaultSpecPath returns the conventional spec path next to the diagram file:
// `arch.drawio` → `arch.cloudcent.yaml`.
func DefaultSpecPath(diagramPath string) string {
	base := diagramPath
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	return base + ".cloudcent.yaml"
}
