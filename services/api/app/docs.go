package app

import (
	"net/http"

	apispec "github.com/primaryrutabaga/ruby-core/api"
)

// serveOpenAPI serves the embedded bundled spec at /openapi.yaml.
func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(apispec.Bundled)
}

// serveDocs serves a Scalar API reference page that renders /openapi.yaml. The
// Scalar runtime is loaded from a pinned CDN URL; vendoring the asset for fully
// offline docs is a possible follow-up.
func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(scalarHTML))
}

const scalarHTML = `<!doctype html>
<html>
  <head>
    <title>ruby-core API</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script id="api-reference" data-url="/openapi.yaml"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.25.0/dist/browser/standalone.min.js"></script>
  </body>
</html>
`
