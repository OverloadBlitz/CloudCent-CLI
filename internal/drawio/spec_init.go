package drawio

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"gopkg.in/yaml.v3"
)

// GenerateSpec walks the diagram's components and builds a Spec template
// suitable for `diagram init`. DrawioResources in metadata drives which
// services are billable, what attrs to expose, and what defaults to pre-fill.
//
// When meta is nil (metadata not yet downloaded), all components are written
// with noPricing: true so the file is still useful as a starting point.
func GenerateSpec(d *Diagram, meta *api.MetadataResponse, defaultRegion string) Spec {
	spec := Spec{
		Version: SpecVersion,
		Defaults: SpecDefaults{
			Provider: "aws",
			Region:   defaultRegion,
		},
	}

	for _, comp := range d.Components {
		entry := SpecComponent{
			ID:      comp.ID,
			Label:   comp.Label,
			Service: comp.ServiceType,
		}
		// Only override the per-component provider when it differs from the
		// spec default — keeps the YAML quiet for the common single-cloud case.
		if comp.Provider != "" && comp.Provider != spec.Defaults.Provider {
			entry.Provider = comp.Provider
		}

		def, found := lookupDrawioDef(comp.ServiceType, meta)
		if !found {
			entry.NoPricing = true
			spec.Components = append(spec.Components, entry)
			continue
		}

		// Build attrs map: pre-fill defaults from the def, leave the rest empty.
		entry.Attrs = make(map[string]string, len(def.Attrs))
		for canonicalName, attrDef := range def.Attrs {
			entry.Attrs[canonicalName] = attrDef.Default // may be ""
		}

		if region := suggestRegion(meta, def.Product, defaultRegion); region != "" && region != defaultRegion {
			entry.Region = region
		}

		spec.Components = append(spec.Components, entry)
	}

	return spec
}

// WriteSpec serializes a Spec to YAML, attaching example-value head comments
// to each attr key when the metadata supplies them.
func WriteSpec(w io.Writer, spec Spec, meta *api.MetadataResponse) error {
	root := &yaml.Node{Kind: yaml.MappingNode}

	appendScalar(root, "version", fmt.Sprintf("%d", spec.Version), "")

	defaultsNode := &yaml.Node{Kind: yaml.MappingNode}
	appendScalar(defaultsNode, "provider", spec.Defaults.Provider, "")
	appendScalar(defaultsNode, "region", spec.Defaults.Region, "")
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "defaults"},
		defaultsNode,
	)

	componentsNode := &yaml.Node{Kind: yaml.SequenceNode}
	for _, comp := range spec.Components {
		componentsNode.Content = append(componentsNode.Content, componentNode(comp, meta))
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "components"},
		componentsNode,
	)

	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(root)
}

func componentNode(comp SpecComponent, meta *api.MetadataResponse) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}

	tag := comp.Service
	if comp.Provider != "" {
		tag = comp.Provider + ":" + comp.Service
	}
	headerLines := []string{fmt.Sprintf("%s [%s]", displayName(comp), tag)}
	if comp.NoPricing {
		headerLines = append(headerLines, "no pricing for this service — leave noPricing: true to skip it")
	}
	n.HeadComment = strings.Join(headerLines, "\n")

	appendScalar(n, "id", comp.ID, "")
	appendScalar(n, "label", comp.Label, "")
	appendScalar(n, "service", comp.Service, "")

	if comp.Provider != "" {
		appendScalar(n, "provider", comp.Provider, "")
	}
	if comp.Region != "" {
		appendScalar(n, "region", comp.Region, "")
	}

	if comp.NoPricing {
		appendScalar(n, "noPricing", "true", "")
		return n
	}

	if len(comp.Attrs) == 0 {
		return n
	}

	// Resolve the def to get canonical attr ordering.
	def, _ := lookupDrawioDef(comp.Service, meta)

	attrsNode := &yaml.Node{Kind: yaml.MappingNode}
	keys := orderAttrKeys(comp.Attrs, def)
	for _, k := range keys {
		v := comp.Attrs[k]
		product := ""
		if def.Product != "" {
			product = def.Product
		}
		comment := exampleComment(meta, product, k)
		appendScalar(attrsNode, k, v, comment)
	}
	n.Content = append(n.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "attrs"},
		attrsNode,
	)

	if comp.Price != "" {
		appendScalar(n, "price", comp.Price, "")
	}

	return n
}

func appendScalar(parent *yaml.Node, key, value, headComment string) {
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	valNode := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	if headComment != "" {
		keyNode.HeadComment = headComment
	}
	parent.Content = append(parent.Content, keyNode, valNode)
}

// orderAttrKeys returns attr keys in a stable order: def-defined keys first
// (in map iteration order, then sorted for determinism), then any extra
// user-added keys sorted alphabetically.
func orderAttrKeys(attrs map[string]string, def api.DrawioResourceDef) []string {
	seen := map[string]bool{}
	// Collect def keys in sorted order for determinism.
	defKeys := make([]string, 0, len(def.Attrs))
	for k := range def.Attrs {
		defKeys = append(defKeys, k)
	}
	sort.Strings(defKeys)

	out := []string{}
	for _, k := range defKeys {
		if _, ok := attrs[k]; ok {
			out = append(out, k)
			seen[k] = true
		}
	}

	// Any extra keys the user added that aren't in the def.
	extras := []string{}
	for k := range attrs {
		if !seen[k] {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	return append(out, extras...)
}

func exampleComment(meta *api.MetadataResponse, product, attr string) string {
	if meta == nil || product == "" {
		return ""
	}
	values, ok := meta.AttributeValues[product][attr]
	if !ok || len(values) == 0 {
		return ""
	}
	preview := values
	if len(preview) > 6 {
		preview = preview[:6]
	}
	return "examples: " + strings.Join(preview, ", ")
}

func suggestRegion(meta *api.MetadataResponse, product, defaultRegion string) string {
	if meta == nil || product == "" {
		return ""
	}
	regions := meta.ProductRegions[product]
	if len(regions) == 0 {
		return ""
	}
	for _, r := range regions {
		if r == defaultRegion {
			return defaultRegion
		}
	}
	return regions[0]
}
