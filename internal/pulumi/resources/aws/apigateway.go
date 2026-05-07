package aws

import (
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

// AddAPIGatewayV2ProtocolType derives pricing attrs from the protocolType input.
// It always sets protocol_type to the uppercased value, and for WEBSOCKET APIs
// also overrides productFamily and operation to the WebSocket variants.
// When the metadata mapping already includes a protocolType attr (mapped directly),
// the derived protocol_type attr is skipped to avoid duplication.
func AddAPIGatewayV2ProtocolType(record resources.ResourceRecord, attrs, props map[string]string) {
	protocolType := ExtractInput(record.Inputs, "protocolType")
	if protocolType == "" {
		return
	}

	upper := strings.ToUpper(protocolType)

	// Only set the derived protocol_type attr when the metadata mapping hasn't
	// already mapped protocolType directly (checked by the caller via attrs).
	if _, alreadyMapped := attrs["protocolType"]; !alreadyMapped {
		attrs["protocol_type"] = upper
		props["protocol_type"] = upper
	}

	if upper == "WEBSOCKET" {
		attrs["productFamily"] = "WebSocket"
		props["productFamily"] = "WebSocket"
		attrs["operation"] = "ApiGatewayWebSocket"
		props["operation"] = "ApiGatewayWebSocket"
	}
}
