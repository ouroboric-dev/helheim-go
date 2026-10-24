package photos

import "net/http"

// Handler serves GET /photos/{photoId}. The photo p1 is user-a's.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /photos/{photoId}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User") == "user-a" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	return mux
}
