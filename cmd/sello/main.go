// Command sello is the local desktop agent (SIF). Skeleton: status page only;
// product slices land per plan E2.
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "app": "facturable-sello"})
	})
	log.Println("sello UI: http://127.0.0.1:8787")
	log.Fatal(http.ListenAndServe("127.0.0.1:8787", mux))
}
