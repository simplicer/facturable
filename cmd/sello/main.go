// Command sello is the local agent (the SIF): CSV ingest, stamping, AEAT
// submission and the ledger. It listens on 127.0.0.1 ONLY.
//
// Environment configuration:
//
//	SELLO_DB                    ruta de la base de datos (defecto var/sello.sqlite)
//	SELLO_NIF / SELLO_NOMBRE    datos de la empresa que factura
//	SELLO_CERT_P12 / SELLO_CERT_PASS  certificado cualificado (sin él, remisor simulado)
//	SELLO_AEAT                  pruebas|produccion (defecto pruebas)
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"facturable/internal/licenses"
	"facturable/internal/sello/declaration"
	"facturable/internal/sello/pdf"
	"facturable/internal/verifactu/domain/qr"

	"facturable/internal/sello"
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/infrastructure/soap"

	sslmatep12 "software.sslmate.com/src/go-pkcs12"
)

func main() {
	version := env("FACTURABLE_VERSION", "0.1.0-unstable")
	db := env("SELLO_DB", "var/sello.sqlite")
	nif := mustEnv("SELLO_NIF")
	nombre := env("SELLO_NOMBRE", nif)

	store, err := persistence.Abrir(db)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	remisor, modo := construirRemisor()

	// license gate (audit #11): stamping requires a valid signed license
	// when LICENSE_TOKEN is configured; dev without it stays open
	var licPub ed25519.PublicKey
	licencia := func() (bool, string) {
		tok := os.Getenv("LICENSE_TOKEN")
		if tok == "" {
			return true, "" // desarrollo sin licencias
		}
		if licPub == nil {
			raw, err := os.ReadFile(env("LICENSE_PUB", "etc/licencias.ed25519.pub"))
			if err != nil {
				return false, "clave pública de licencias no disponible"
			}
			licPub = ed25519.PublicKey(raw)
		}
		lic, err := licenses.Verify(licPub, tok)
		if err != nil {
			return false, err.Error()
		}
		if !lic.AuthorizesNIF(nif) {
			return false, "la licencia no autoriza este NIF"
		}
		if err := licenses.Gate(lic, time.Now(), licenses.ActionStampNew); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
	srv := &sello.Service{
		Store: store, Remisor: remisor,
		Flujo:     application.NewControlFlujo(time.Now),
		EmisorNIF: nif, EmisorNombre: nombre,
		Sistema: application.SistemaLocal{
			NombreProductor: "Facturable", NIFProductor: "B00000000", // productor real: T-8.1
			NombreSistema: "Facturable Sello", IDSistema: "FS",
			Version: version, NumeroInstalacion: "00001",
		},
	}

	// Relay E2E opcional: con RELAY_API_URL el agente publica su clave
	// X25519 y pulla sobres cifrados cada minuto (el servidor no puede
	// leerlos: PRODUCTO-MVP §10.4).
	var relayCli *sello.RelayClient
	if apiURL := os.Getenv("RELAY_API_URL"); apiURL != "" {
		priv, err := sello.LoadOrCreateRelayKey(filepath.Dir(db))
		if err != nil {
			log.Fatalf("relay key: %v", err)
		}
		relayCli = sello.NewRelayClient(apiURL, nif, priv)
		relayCli.Service = srv
		if err := relayCli.Register(context.Background()); err != nil {
			log.Printf("relay register: %v", err)
		} else {
			log.Printf("relay activo contra %s", apiURL)
		}
	}

	// Planificador de remisión: la AEAT exige esperar entre envíos; un ciclo
	// por minuto encaja con el control de flujo (spec AEAT §6.4.4.1).
	go func() {
		for range time.Tick(60 * time.Second) {
			n, err := srv.SubmitPending(context.Background())
			if err != nil {
				log.Printf("remisión: %v", err)
			} else if n > 0 {
				log.Printf("remisión: %d chainRow(s) enviados", n)
			}
		}
	}()

	pin := os.Getenv("SELLO_PIN")
	certTries := newRateLimiter(5, time.Minute)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/certificates", func(w http.ResponseWriter, r *http.Request) {
		if pin != "" && !certTries.allow(r.RemoteAddr) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Too Many Requests", "status": 429, "detail": "demasiados intentos; espera un minuto"})
			return
		}
		saveCertificate(w, r)
	})
	mux.HandleFunc("GET /v1/connectivity", checkConnectivity)
	mux.HandleFunc("GET /v1/declaration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(declaration.Text(declaration.Info{
			ProducerName:    "Facturable",
			ProducerNIF:     "B00000000", // datos definitivos del productor: T-8.1
			ProducerContact: "soporte@facturable.es",
			SystemName:      "Facturable Sello", SystemID: "FS",
			Version: version, Installation: "00001",
			OnlyVerifactu: true, MultiOT: false,
		})))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "app": "facturable-sello", "modo": modo})
	})
	mux.HandleFunc("POST /v1/records", func(w http.ResponseWriter, r *http.Request) {
		if ok, motivo := licencia(); !ok {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title": "licencia", "status": 402, "detail": motivo})
			return
		}
		res, err := srv.IngestCSV(r.Body)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /v1/submissions", func(w http.ResponseWriter, _ *http.Request) {
		n, err := srv.SubmitPending(context.Background())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]int{"enviados": n})
	})
	mux.HandleFunc("GET /v1/records/{id}/pdf", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, fmt.Errorf("identificador de chainRow no válido"))
			return
		}
		f, ok, err := store.ByID(id)
		if err != nil || !ok {
			writeErr(w, fmt.Errorf("no existe el chainRow %d", id))
			return
		}
		qrEnv := qr.Production
		if os.Getenv("SELLO_AEAT") != "produccion" {
			qrEnv = qr.Testing
		}
		urlQR, err := qr.VerificationURL(qrEnv, qr.Query{
			NIF: f.NIF, InvoiceNumber: f.Serie, ExpeditionDate: f.Date, TotalAmount: f.Importe,
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		b, err := pdf.Factura(pdf.DatosFactura{
			EmpresaNombre: nombre, EmpresaNIF: nif,
			Serie: f.Serie, Date: f.Date, Tipo: f.Tipo,
			ClienteNIF: "",
			Desgloses:  []pdf.DesgloseFactura{{Tipo: "", Base: importeMenosCuota(f.Importe, f.Cuota), Cuota: f.Cuota}},
			CuotaTotal: f.Cuota, ImporteTotal: f.Importe,
			EstadoAEAT: f.Estado, CSV: f.CSV, URLCotejo: urlQR,
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(b)
	})

	// Colección de registros: representación por query (JSON por defecto,
	// CSV para la gestoría con formato=csv + filtros por fecha).
	mux.HandleFunc("GET /v1/records", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("format") == "csv" {
			from, to := q.Get("from"), q.Get("to")
			if from == "" {
				from = "01-01-2020"
			}
			if to == "" {
				to = time.Now().Format("02-01-2006")
			}
			b, _, err := srv.ExportPeriod(from, to)
			if err != nil {
				writeErr(w, err)
				return
			}
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=libro-facturas.csv")
			_, _ = w.Write(b)
			return
		}
		filas, err := store.Ledger(nif, 0, 1_000_000_000, 1000)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"data": filas})
	})

	// Auth local (audit high): SELLO_PIN protege /v1/* con Bearer; status y
	// connectivity quedan abiertos para diagnóstico.
	handler := withPIN(pin, mux)

	addr := env("SELLO_LISTEN", "127.0.0.1:8787")
	log.Printf("sello escuchando en http://%s (modo %s)", addr, modo)
	log.Fatal(http.ListenAndServe(addr, handler))
}

// rateLimiter simple de ventana fija por clave (audit: límite subidas cert).
type rateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	seen   map[string]int
	start  map[string]time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{window: window, max: max, seen: map[string]int{}, start: map[string]time.Time{}}
}

func (r *rateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if st, ok := r.start[key]; !ok || now.Sub(st) > r.window {
		r.start[key], r.seen[key] = now, 0
	}
	r.seen[key]++
	return r.seen[key] <= r.max
}

// withPIN exige Authorization: Bearer <SELLO_PIN> en las rutas /v1/*.
func withPIN(pin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pin != "" && strings.HasPrefix(r.URL.Path, "/v1/") &&
			r.Header.Get("Authorization") != "Bearer "+pin {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title": "Unauthorized", "status": 401, "detail": "PIN ausente o incorrecto", "instance": r.URL.Path})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// construirRemisor devuelve el remisor AEAT real cuando hay certificado, o
// uno simulado en desarrollo (todo correcto) cuando no lo hay.
func construirRemisor() (application.Remisor, string) {
	p12 := os.Getenv("SELLO_CERT_P12")
	if p12 == "" {
		return fakeSubmitter{}, "simulado (sin certificado)"
	}
	cert, err := soap.LoadP12(p12, os.Getenv("SELLO_CERT_PASS"))
	if err != nil {
		log.Fatalf("certificado: %v", err)
	}
	produccion := os.Getenv("SELLO_AEAT") == "produccion"
	url := soap.Pruebas.VerifactuPersona
	if produccion {
		url = soap.Produccion.VerifactuPersona
	}
	if os.Getenv("SELLO_CERT_TIPO") == "sello" { // seal certs hit www10
		if produccion {
			url = soap.Produccion.VerifactuSello
		} else {
			url = soap.Pruebas.VerifactuSello
		}
	}
	return soap.NewCliente(url, cert), "aeat"
}

// fakeSubmitter acepta cualquier XML devolviendo un veredicto correcto:
// permite probar el circuito completo sin certificado ni red.
type fakeSubmitter struct{}

func (fakeSubmitter) Remitir(_ context.Context, soapXML string) (application.RespuestaLote, error) {
	serie, fecha := seriesAndDateFromXML(soapXML)
	return application.RespuestaLote{
		EstadoGlobal: "Correcto", CSV: "SIMULADO0000000A",
		Lineas: []application.RespuestaLinea{{
			NIFEmisor: os.Getenv("SELLO_NIF"), NumSerie: serie, Date: fecha,
			TipoOperacion: "Alta", Estado: "Correcto"}},
	}, nil
}

// seriesAndDateFromXML extrae la primera IDFactura del sobre (sólo simulación).
func seriesAndDateFromXML(xmlStr string) (serie, fecha string) {
	extraer := func(ini, fin string) string {
		i := strings.Index(xmlStr, ini)
		if i < 0 {
			return ""
		}
		v := xmlStr[i+len(ini):]
		if j := strings.Index(v, fin); j >= 0 {
			return v[:j]
		}
		return ""
	}
	serie = extraer("<sum1:NumSerieFactura>", "</sum1:NumSerieFactura>")
	fecha = extraer("<sum1:FechaExpedicionFactura>", "</sum1:FechaExpedicionFactura>")
	return serie, fecha
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

// importeMenosCuota resta céntimos a céntimos para mostrar la base.
func importeMenosCuota(importe, cuota string) string {
	restar := func(a, b string) string {
		ai, _ := strconv.Atoi(strings.Replace(a, ".", "", 1))
		bi, _ := strconv.Atoi(strings.Replace(b, ".", "", 1))
		r := ai - bi
		return fmt.Sprintf("%d.%02d", r/100, r%100)
	}
	return restar(importe, cuota)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title": "Error del agente", "status": 400, "detail": err.Error()})
}

// onboarding de certificado -------------------------------------------------

// saveCertificate recibe un P12 por multipart y lo instala (0600) para
// el próximo arranque. Contraseña por formulario aparte: nunca se guarda.
func saveCertificate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, fmt.Errorf("no se pudo leer el formulario: %v", err))
		return
	}
	f, _, err := r.FormFile("certificado")
	if err != nil {
		writeErr(w, fmt.Errorf("falta el fichero 'certificado' (P12/PFX)"))
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := sslmatep12.Decode(raw, r.FormValue("password")); err != nil {
		writeErr(w, fmt.Errorf("el certificado no es un P12 válido o la contraseña no es correcta: %v", err))
		return
	}
	if err := os.MkdirAll("var/certificados", 0o700); err != nil {
		writeErr(w, err)
		return
	}
	dest := filepath.Join("var/certificados", "empresa.p12")
	if err := os.WriteFile(dest, raw, 0o600); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"guardado": dest,
		"nota":     "reinicia el agente con SELLO_CERT_P12=" + dest + " para remitir a la AEAT",
	})
}

// checkConnectivity verifica carga del certificado y handshake TLS contra el
// entorno de pruebas de la AEAT (prewww1). Diagnóstico de onboarding.
func checkConnectivity(w http.ResponseWriter, _ *http.Request) {
	resultado := map[string]any{}
	p12 := os.Getenv("SELLO_CERT_P12")
	if p12 != "" {
		if _, err := soap.LoadP12(p12, os.Getenv("SELLO_CERT_PASS")); err != nil {
			resultado["certificado"] = "error: " + err.Error()
			writeErr(w, fmt.Errorf("%v", resultado["certificado"]))
			return
		}
		resultado["certificado"] = "ok"
	} else {
		resultado["certificado"] = "no configurado (modo simulado)"
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp",
		"prewww1.aeat.es:443", &tls.Config{MinVersion: tls.VersionTLS12})
	if err != nil {
		resultado["aeat_pruebas"] = "error: " + err.Error()
		writeJSON(w, resultado)
		return
	}
	_ = conn.Close()
	resultado["aeat_pruebas"] = "ok"
	writeJSON(w, resultado)
}
