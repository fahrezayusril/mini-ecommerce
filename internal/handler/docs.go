package handler

import (
	"log"
	"net/http"
)

const DocsPath = "/docs"

const swaggerUIPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Mini E-Commerce API Docs</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css" integrity="sha384-wxLW6kwyHktdDGr6Pv1zgm/VGJh99lfUbzSn6HNHBENZlCN7W602k9VkGdxuFvPn" crossorigin="anonymous">
  <style>body { margin: 0; background: #fafafa; }</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js" integrity="sha384-wmyclcVGX/WhUkdkATwhaK1X1JtiNrr2EoYJ+diV3vj4v6OC5yCeSu+yW13SYJep" crossorigin="anonymous"></script>
  <script>
    window.addEventListener("load", function () {
      window.ui = SwaggerUIBundle({
        url: "/docs/openapi.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        persistAuthorization: true
      });
    });
  </script>
</body>
</html>
`

type DocsHandler struct {
	spec []byte
}

func NewDocsHandler(spec []byte) *DocsHandler {
	return &DocsHandler{spec: spec}
}

func (h *DocsHandler) UI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(swaggerUIPage)); err != nil {
		log.Printf("write docs page: %v", err)
	}
}

func (h *DocsHandler) Spec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(h.spec); err != nil {
		log.Printf("write openapi spec: %v", err)
	}
}

func (h *DocsHandler) RedirectToUI(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, DocsPath, http.StatusMovedPermanently)
}
