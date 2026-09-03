package main

import (
	"net/http"
)

func HTTPAuthenticator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// stub
		http.Error(w, "auth not implemented", http.StatusNotImplemented)
		return
		// end stub

		next.ServeHTTP(w, r)
	})
}
