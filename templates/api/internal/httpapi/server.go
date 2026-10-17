// Package httpapi is the API's HTTP surface: the routes, how they answer, and how they fail.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
)

// Pinger is what the server needs to know whether the database is there.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are what the routes use. A field that is nil is a part the server runs without.
type Deps struct {
	DB Pinger
}

// New returns the handler of every route. A route is added here and described in openapi.yaml, in the
// same change.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", only(http.MethodGet, healthz(d)))
	mux.HandleFunc("/", notFound)
	return mux
}

// only answers 405 to every method but method. A catch-all route would otherwise answer a wrong method
// on a known path with 404 (the mux gives a method-specific pattern no 405 beside "/").
func only(method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		h(w, r)
	}
}

func healthz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.DB != nil && d.DB.Ping(r.Context()) != nil {
			// What is wrong stays in the server's log; a caller learns only that the database is not there.
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// writeJSON answers with status and v as JSON. A value that can't be encoded is a bug in the code that
// built it, so it answers 500 rather than a half-written body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
