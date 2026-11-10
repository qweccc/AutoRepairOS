package routes

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNewRegistersPhase1Routes(t *testing.T) {
	r := New(Deps{})
	var paths []string
	chi.Walk(r, func(method, route string, h http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		paths = append(paths, method+" "+route)
		return nil
	})
	want := map[string]bool{
		"GET /api/parts":              true,
		"GET /api/customers/search":   true,
		"GET /api/inventory/search":   true,
		"GET /api/work-orders/search": true,
		"GET /api/vehicles/search":    true,
	}
	for _, p := range paths {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("missing routes: %v", want)
	}
}
