// Package api serves the domain model over HTTP.
package api

import (
	"net/http"

	"example.com/imports-good/core"
)

// Handler greets the user named in the query.
func Handler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(core.Name(r.URL.Query().Get("name"))))
}
