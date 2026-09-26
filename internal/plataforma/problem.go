package plataforma

import "net/http"

// Problem writes an application/problem+json response (RFC 9457 style).
func Problem(w http.ResponseWriter, status int, title, detail, instance string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	body := map[string]any{"title": title, "status": status, "detail": detail, "instance": instance}
	_ = json.NewEncoder(w).Encode(body)
}
