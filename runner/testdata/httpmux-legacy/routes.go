// Package routes declares go 1.21, before ServeMux understood methods and
// wildcards. The default selection runs httpmux, which reports the pattern
// below: under go 1.21 it is a literal path that no request matches.
package routes

import "net/http"

// Register adds the item handler to mux.
func Register(mux *http.ServeMux, item http.HandlerFunc) {
	mux.HandleFunc("GET /items/{id}", item)
}
