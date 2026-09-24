package httpapi

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// dashboardCSP allows only same-origin code: the dashboard is a static bundle
// that talks exclusively to this API.
const dashboardCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		headers := response.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		if strings.HasPrefix(request.URL.Path, "/v1/") {
			headers.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(response, request)
	})
}

// mountDashboard serves the single-page dashboard at /app/. Unknown paths fall
// back to index.html so client-side routes survive reloads; hashed assets are
// cached immutably while the HTML entry point is always revalidated.
func mountDashboard(router chi.Router, dir string) {
	root := http.Dir(dir)
	files := http.FileServer(root)
	index := filepath.Join(dir, "index.html")
	router.Get("/", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/app/", http.StatusFound)
	})
	router.Get("/app", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/app/", http.StatusMovedPermanently)
	})
	router.Get("/app/*", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", dashboardCSP)
		relative := path.Clean("/" + strings.TrimPrefix(request.URL.Path, "/app/"))
		if relative != "/" {
			if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(relative))); err == nil && !info.IsDir() {
				if strings.HasPrefix(relative, "/assets/") {
					response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				request = request.Clone(request.Context())
				request.URL.Path = relative
				files.ServeHTTP(response, request)
				return
			}
			if path.Ext(relative) != "" {
				http.NotFound(response, request)
				return
			}
		}
		response.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(response, request, index)
	})
}
