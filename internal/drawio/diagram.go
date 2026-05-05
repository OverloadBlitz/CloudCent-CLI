package drawio

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// htmlTagRegexp matches any HTML tag (`<...>`) including ones with attributes.
// Used to strip HTML markup that draw.io embeds in cell labels.
var htmlTagRegexp = regexp.MustCompile(`<[^>]*>`)

// whitespaceRegexp collapses runs of whitespace into a single space.
var whitespaceRegexp = regexp.MustCompile(`\s+`)

// Component is a parsed draw.io element.
type Component struct {
	ID          string
	ParentID    string // draw.io parent cell ID (for nesting)
	Label       string
	Style       string
	X, Y        float64
	Width       float64
	Height      float64
	ServiceType string // shape suffix (e.g. "s3", "elastic_ip_address"); "" if unrecognised
	Provider    string // cloud provider derived from shape prefix (e.g. "aws", "azure", "gcp", "oci"); "" if unknown
}

// Diagram is the result of parsing a .drawio XML file.
type Diagram struct {
	Components []Component
	Edges      [][2]string // [source_id, target_id]
}

// ParseFile reads and parses a .drawio file.
func ParseFile(path string) (*Diagram, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read diagram file %q: %w", path, err)
	}
	return Parse(string(data))
}

// decodeDiagramContent decodes the inner content of a <diagram> element.
//
// draw.io desktop saves diagrams as:
//
//	base64( deflate( encodeURIComponent( xml ) ) )
//
// So the decode pipeline is:
//  1. base64-decode  (standard alphabet, padding optional)
//  2. raw deflate decompress (no zlib header)
//  3. URL-decode the resulting string to get plain XML
//
// If the content already looks like XML it is returned as-is.
func decodeDiagramContent(encoded string) (string, error) {
	encoded = strings.TrimSpace(encoded)
	if strings.HasPrefix(encoded, "<") {
		return encoded, nil
	}

	// Step 1: base64-decode (accept both padded and unpadded)
	compressed, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(encoded, "="))
	if err != nil {
		// Fall back to standard (padded) decoding
		compressed, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("base64 decode failed: %w", err)
		}
	}

	// Step 2: raw deflate decompress (no zlib header, wbits = -15)
	r := flate.NewReader(bytes.NewReader(compressed))
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("deflate decompress failed: %w", err)
	}

	// Step 3: URL-decode (draw.io runs encodeURIComponent before compressing)
	xmlStr, err := url.QueryUnescape(string(out))
	if err != nil {
		// Not URL-encoded — use raw decompressed output
		xmlStr = string(out)
	}

	return xmlStr, nil
}

// extractDiagramContent extracts and decodes the content inside the first
// <diagram>…</diagram> element, if present. Otherwise returns the input unchanged.
func extractDiagramContent(input string) (string, error) {
	open := strings.Index(input, "<diagram")
	if open < 0 {
		return input, nil
	}
	contentStart := strings.Index(input[open:], ">")
	if contentStart < 0 {
		return input, nil
	}
	contentStart += open + 1

	close := strings.Index(input[contentStart:], "</diagram>")
	if close < 0 {
		return input, nil
	}
	inner := input[contentStart : contentStart+close]
	return decodeDiagramContent(inner)
}

// Parse parses draw.io XML and extracts components.
// It handles both plain XML and the compressed/base64-encoded format used by
// the draw.io desktop app (deflate + base64 inside <diagram>).
func Parse(xml string) (*Diagram, error) {
	// If the file wraps content in <diagram>, decode it first.
	decoded, err := extractDiagramContent(xml)
	if err != nil {
		return nil, fmt.Errorf("failed to decode diagram content: %w", err)
	}
	xml = decoded

	d := &Diagram{}
	pos := 0
	for pos < len(xml) {
		start := strings.Index(xml[pos:], "<mxCell")
		if start < 0 {
			break
		}
		absStart := pos + start

		// Find the end of the mxCell element (either self-closing or with children).
		// First locate the end of the opening tag.
		tagEnd := strings.Index(xml[absStart:], ">")
		if tagEnd < 0 {
			break
		}
		absTagEnd := absStart + tagEnd + 1
		tag := xml[absStart:absTagEnd]

		// If the opening tag is not self-closing, look for </mxCell> to get the
		// full element so we can find a nested <mxGeometry> child.
		var fullElement string
		if strings.HasSuffix(strings.TrimSpace(tag), "/>") {
			fullElement = tag
		} else {
			closeTag := strings.Index(xml[absTagEnd:], "</mxCell>")
			if closeTag >= 0 {
				fullElement = xml[absStart : absTagEnd+closeTag+len("</mxCell>")]
			} else {
				fullElement = tag
			}
		}

		id := extractAttr(tag, "id")
		// draw.io uses "value" for the cell label; some exports use "label"
		label := extractAttr(tag, "value")
		if label == "" {
			label = extractAttr(tag, "label")
		}
		label = cleanLabel(label)
		style := filterStyle(extractAttr(tag, "style"))
		source := extractAttr(tag, "source")
		target := extractAttr(tag, "target")

		// Geometry may be inline on <mxCell> or on a nested <mxGeometry> child.
		x, y, w, h := extractGeometryFromElement(tag, fullElement)

		isEdge := strings.Contains(tag, `edge="1"`) || (source != "" && target != "")
		isVertex := strings.Contains(tag, `vertex="1"`)

		if isEdge {
			if source != "" && target != "" {
				d.Edges = append(d.Edges, [2]string{source, target})
			}
		} else if isVertex && id != "0" && id != "1" && label != "" {
			parent := extractAttr(tag, "parent")
			service, provider := DetectService(label, style)
			d.Components = append(d.Components, Component{
				ID:          id,
				ParentID:    parent,
				Label:       label,
				Style:       style,
				X:           x,
				Y:           y,
				Width:       w,
				Height:      h,
				ServiceType: service,
				Provider:    provider,
			})
		}
		pos = absTagEnd
	}

	sort.Slice(d.Components, func(i, j int) bool {
		ay := int64(d.Components[i].Y / 50)
		by := int64(d.Components[j].Y / 50)
		if ay != by {
			return ay < by
		}
		return int64(d.Components[i].X) < int64(d.Components[j].X)
	})

	return d, nil
}

func extractAttr(tag, attr string) string {
	// Match " attr=" or start-of-string attr= to avoid partial matches
	// e.g. "x=" must not match inside "vertex=" or "maxX="
	needle := attr + `="`
	pos := 0
	for pos < len(tag) {
		idx := strings.Index(tag[pos:], needle)
		if idx < 0 {
			return ""
		}
		abs := pos + idx
		// Check that the character before the match is a space, tab, or start
		if abs > 0 {
			prev := tag[abs-1]
			if prev != ' ' && prev != '\t' && prev != '\n' {
				// partial match inside another attribute name — skip
				pos = abs + len(needle)
				continue
			}
		}
		start := abs + len(needle)
		end := strings.Index(tag[start:], `"`)
		if end < 0 {
			return ""
		}
		return tag[start : start+end]
	}
	return ""
}

func extractGeometry(tag string) (x, y, w, h float64) {
	x = parseFloat(extractAttr(tag, "x"), 0)
	y = parseFloat(extractAttr(tag, "y"), 0)
	w = parseFloat(extractAttr(tag, "width"), 120)
	h = parseFloat(extractAttr(tag, "height"), 60)
	return
}

// extractGeometryFromElement tries to get geometry from the mxCell opening tag
// first, then falls back to a nested <mxGeometry> child element.
func extractGeometryFromElement(tag, fullElement string) (x, y, w, h float64) {
	// Try inline attributes on <mxCell> first.
	if extractAttr(tag, "x") != "" || extractAttr(tag, "width") != "" {
		return extractGeometry(tag)
	}
	// Look for <mxGeometry> child.
	geoStart := strings.Index(fullElement, "<mxGeometry")
	if geoStart >= 0 {
		geoEnd := strings.Index(fullElement[geoStart:], ">")
		if geoEnd >= 0 {
			geoTag := fullElement[geoStart : geoStart+geoEnd+1]
			return extractGeometry(geoTag)
		}
	}
	return extractGeometry(tag)
}

// cleanLabel strips HTML markup and entity escapes from a draw.io cell label.
// draw.io stores labels as HTML fragments, so values commonly look like
// `Amazon&lt;br style=&quot;font-size: 16px;&quot;&gt;S3` or
// `&lt;font style="font-size: 19px"&gt;RDS Master&lt;/font&gt;`. We unescape
// entities, drop every `<...>` tag, and collapse whitespace.
func cleanLabel(label string) string {
	if label == "" {
		return ""
	}
	label = html.UnescapeString(label)
	label = htmlTagRegexp.ReplaceAllString(label, " ")
	label = whitespaceRegexp.ReplaceAllString(label, " ")
	return strings.TrimSpace(label)
}

// filterStyle reduces a draw.io style string to only the keys that identify
// the shape: `shape`, `grIcon`, `resIcon`, and `prIcon`. All visual
// properties (fillColor, strokeColor, fontSize, etc.) are discarded.
func filterStyle(style string) string {
	if style == "" {
		return ""
	}
	keep := map[string]bool{"shape": true, "grIcon": true, "resIcon": true, "prIcon": true}
	parts := strings.Split(style, ";")
	out := make([]string, 0, 4)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			continue
		}
		if keep[p[:eq]] {
			out = append(out, p)
		}
	}
	return strings.Join(out, ";")
}

func parseFloat(s string, def float64) float64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

// shapeProviders maps a drawio stencil prefix to the cloud provider that
// owns it. The stencil naming convention is `mxgraph.<library>.<service>`,
// so the suffix after the prefix is the service identifier. AWS has three
// generations of stencils (aws2/3/4) — they all map back to provider "aws".
var shapeProviders = map[string]string{
	"mxgraph.aws4.":  "aws",
	"mxgraph.aws3.":  "aws",
	"mxgraph.aws2.":  "aws",
	"mxgraph.aws.":   "aws",
	"mxgraph.azure.": "azure",
	"mxgraph.gcp.":   "gcp",
	"mxgraph.oci.":   "oci",
}

// genericShapeWrappers are stencils that hold a real icon as a child
// (resIcon / grIcon). They aren't a service themselves and should be skipped.
var genericShapeWrappers = map[string]bool{
	"resourceicon": true,
	"groupcenter":  true,
	"producticon":  true,
	"group":        true,
}

// DetectService returns the cloud-service identifier and provider carried
// by the drawio cell's style, e.g. ("s3", "aws") or ("virtual_machine",
// "azure"). We look at `resIcon`, `grIcon`, `prIcon`, then `shape` in that
// order — the icon attributes win because generic AWS4 wrapper cells use
// `shape=mxgraph.aws4.resourceIcon` (or `productIcon`/`groupCenter`) with
// the real service stored in the corresponding `*Icon=mxgraph.aws4.<service>`
// attribute. If no shape-based service can be derived, both return values
// are empty. The label is intentionally ignored: hand-typed text is
// unreliable and would require a hardcoded keyword table.
func DetectService(label, style string) (service, provider string) {
	if style == "" {
		return "", ""
	}
	for _, key := range []string{"resIcon", "grIcon", "prIcon", "shape"} {
		raw := styleValue(style, key)
		if raw == "" {
			continue
		}
		suffix, prov := splitShape(raw)
		if suffix == "" || genericShapeWrappers[strings.ToLower(suffix)] {
			continue
		}
		return suffix, prov
	}
	return "", ""
}

// styleValue extracts the value of `key=` from a draw.io style string.
// Style is a `;`-separated list of `key=value` segments and bare flags.
func styleValue(style, key string) string {
	for _, part := range strings.Split(style, ";") {
		part = strings.TrimSpace(part)
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		if part[:eq] == key {
			return part[eq+1:]
		}
	}
	return ""
}

// splitShape splits a stencil shape value into (service-suffix, provider).
// Provider is derived from the `mxgraph.<library>.` prefix. Values without
// a known provider prefix are returned with provider="" and the original
// value lower-cased as the suffix (so callers can still display something).
func splitShape(shape string) (suffix, provider string) {
	s := strings.ToLower(strings.TrimSpace(shape))
	for prefix, prov := range shapeProviders {
		if strings.HasPrefix(s, prefix) {
			return s[len(prefix):], prov
		}
	}
	return s, ""
}
