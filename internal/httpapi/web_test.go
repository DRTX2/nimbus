package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The dashboard handler reads from disk using request paths, so traversal and
// fallback behavior are verified through the real router.
func TestDashboardServingBoundaries(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<html>app</html>", "assets/app-1.js": "js"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(filepath.Dir(dir), "outside.txt")
	_ = os.WriteFile(secret, []byte("secret"), 0o600)
	t.Cleanup(func() { _ = os.Remove(secret) })

	router := chi.NewRouter()
	router.Use(securityHeaders)
	mountDashboard(router, dir)

	tests := []struct {
		path, wantBody, wantCache string
		wantStatus                int
	}{
		{path: "/app/", wantStatus: 200, wantBody: "app", wantCache: "no-cache"},
		{path: "/app/incidents/123", wantStatus: 200, wantBody: "app", wantCache: "no-cache"},
		{path: "/app/assets/app-1.js", wantStatus: 200, wantBody: "js", wantCache: "immutable"},
		{path: "/app/../outside.txt", wantStatus: 404},
		{path: "/app/missing.js", wantStatus: 404},
		{path: "/", wantStatus: http.StatusFound},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if strings.Contains(recorder.Body.String(), "secret") {
				t.Fatal("served a file outside the dashboard directory")
			}
			if test.wantBody != "" && !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Fatalf("body = %q", recorder.Body.String())
			}
			if test.wantCache != "" && !strings.Contains(recorder.Header().Get("Cache-Control"), test.wantCache) {
				t.Fatalf("cache-control = %q", recorder.Header().Get("Cache-Control"))
			}
			if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff header")
			}
		})
	}
}
