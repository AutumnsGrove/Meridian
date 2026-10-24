// Fixture for the one mutating route rule: the launcher gains an action.
package gateway

import "net/http"

func routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/restart", nil)
}
