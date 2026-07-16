package httpapi

import (
	"crypto/subtle"
	"net/http"
)

func validCSRF(expected, supplied string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) == 1
}

func setDashboardHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
}
