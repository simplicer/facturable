// Command sello es el agente local (el SIF): ingesta CSV, sella, remite a
// la AEAT y expone el libro. Escucha SOLO en 127.0.0.1.
//
// Configuración por entorno:
//
//	SELLO_DB                    ruta de la base de datos (defecto var/sello.sqlite)
//	SELLO_NIF / SELLO_NOMBRE    datos de la empresa que factura
//	SELLO_CERT_P12 / SELLO_CERT_PASS  certificado cualificado (sin él, remisor simulado)
//	SELLO_AEAT                  pruebas|produccion (defecto pruebas)
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"facturable/internal/sello"
	"facturable/internal/sello/persistencia"
	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/infrastructure/soap"
)

func main() {
	db := env("SELLO_DB", "var/sello.sqlite")
	nif := mustEnv("SELLO_NIF")
	nombre := env("SELLO_NOMBRE", nif)

	store, err := persistencia.Abrir(db)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	remisor, modo := construirRemisor()
	srv := &sello.Servicio{
		Store: store, Remisor: remisor,
		Flujo:     application.NewControlFlujo(time.Now),
		EmisorNIF: nif, EmisorNombre: nombre,
		Sistema: application.SistemaLocal{
			NombreProductor: "Facturable", NIFProductor: "B00000000", // productor real: T-8.1
			NombreSistema: "Facturable Sello", IDSistema: "FS",
			Version: "0.1.0", NumeroInstalacion: "00001",
		},
	}

	// Planificador de remisión: la AEAT exige esperar entre envíos; un ciclo
	// por minuto encaja con el control de flujo (spec AEAT §6.4.4.1).
	go func() {
		for range time.Tick(60 * time.Second) {
			n, err := srv.EnviarPendientes(context.Background())
			if err != nil {
				log.Printf("remisión: %v", err)
			} else if n > 0 {
				log.Printf("remisión: %d registro(s) enviados", n)
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "app": "facturable-sello", "modo": modo})
	})
	mux.HandleFunc("POST /v1/ingesta", func(w http.ResponseWriter, r *http.Request) {
		res, err := srv.IngestaCSV(r.Body)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /v1/enviar", func(w http.ResponseWriter, _ *http.Request) {
		n, err := srv.EnviarPendientes(context.Background())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]int{"enviados": n})
	})
	mux.HandleFunc("GET /v1/registros", func(w http.ResponseWriter, _ *http.Request) {
		filas, err := store.Listado(nif, 0, 1_000_000_000, 1000)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"data": filas})
	})

	addr := env("SELLO_LISTEN", "127.0.0.1:8787")
	log.Printf("sello escuchando en http://%s (modo %s)", addr, modo)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// construirRemisor devuelve el remisor AEAT real cuando hay certificado, o
// uno simulado en desarrollo (todo correcto) cuando no lo hay.
func construirRemisor() (application.Remisor, string) {
	p12 := os.Getenv("SELLO_CERT_P12")
	if p12 == "" {
		return remisorSimulado{}, "simulado (sin certificado)"
	}
	cert, err := soap.LoadP12(p12, os.Getenv("SELLO_CERT_PASS"))
	if err != nil {
		log.Fatalf("certificado: %v", err)
	}
	url := soap.Pruebas.VerifactuPersona
	if os.Getenv("SELLO_AEAT") == "produccion" {
		url = soap.Produccion.VerifactuPersona
	}
	return soap.NewCliente(url, cert), "aeat"
}

// remisorSimulado acepta cualquier XML devolviendo un veredicto correcto:
// permite probar el circuito completo sin certificado ni red.
type remisorSimulado struct{}

func (remisorSimulado) Remitir(_ context.Context, _ string) (application.RespuestaLote, error) {
	return application.RespuestaLote{
		EstadoGlobal: "Correcto", CSV: "SIMULADO0000000A",
		Lineas: []application.RespuestaLinea{{Estado: "Correcto"}},
	}, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("falta la variable de entorno %s (NIF de la empresa que factura)", k)
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title": "Error del agente", "status": 400, "detail": err.Error()})
}
