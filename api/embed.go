// Package apispec embeds the bundled OpenAPI document so the API service can serve
// it at /openapi.yaml without a runtime file dependency. The bundle is generated
// from the per-domain fragments under api/openapi/ (see ADR-0041); regenerate with
// `make openapi-gen`.
package apispec

import _ "embed"

// Bundled is the byte content of the bundled OpenAPI spec (api/openapi.gen.yaml).
//
//go:embed openapi.gen.yaml
var Bundled []byte
