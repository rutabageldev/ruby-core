// Package oas contains the ogen-generated HTTP server, client, and types for the
// ruby-core read API. Everything here except this file is generated from the bundled
// OpenAPI spec (api/openapi.gen.yaml) and MUST NOT be edited by hand — regenerate via
// `make openapi-gen` (or `go generate ./services/api/oas`). See ADR-0041.
package oas

//go:generate go run github.com/ogen-go/ogen/cmd/ogen@v1.22.0 --target . --package oas --clean ../../../api/openapi.gen.yaml
