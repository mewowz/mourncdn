package main

import (
	"net/http"
)

func HTTPAuthenticator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// stub

		next.ServeHTTP(w, r)
	})
}
