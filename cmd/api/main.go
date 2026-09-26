// Command api serves the public API: /status, /version (licensing, relay and
// payments arrive in later slices; see docs/PLAN-DESARROLLO.md E3/E4).
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"facturable/internal/licenses"
	"facturable/internal/plataforma"

	_ "modernc.org/sqlite"
)

func main() {
	cfg, err := plataforma.LoadConfig(envOr("FACTURABLE_CONFIG", "etc/config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log, err := plataforma.NewLogger(cfg.Log.Dir, cfg.Log.Level)
	if err != nil {
		fmt.Fprintln(os.Stderr, "log:", err)
		os.Exit(1)
	}
	db, err := sql.Open("sqlite", cfg.API.Database)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bd:", err)
		os.Exit(1)
	}
	defer db.Close()
	priv, _, err := licenses.LoadOrCreateKey("etc/licenses.ed25519")
	if err != nil {
		fmt.Fprintln(os.Stderr, "claves:", err)
		os.Exit(1)
	}
	apiLic, err := licenses.NuevaAPI(db, priv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "licencias:", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	apiLic.Mount(mux)
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	version := os.Getenv("FACTURABLE_VERSION")
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"version": version}})
	})
	log.Info("api listening", map[string]any{"listen": cfg.API.Listen, "env": cfg.Env})
	if err := http.ListenAndServe(cfg.API.Listen, mux); err != nil {
		log.Error("api stopped", map[string]any{"error": err.Error()})
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
