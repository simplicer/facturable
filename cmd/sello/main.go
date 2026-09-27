// Command sello is the local agent (the SIF): CSV ingest, stamping, AEAT
// submission and the ledger. It listens on 127.0.0.1 ONLY.
//
// Environment configuration:
//
//	SELLO_DB                    ruta de la base de datos (defecto var/sello.sqlite)
//	SELLO_NIF / SELLO_NOMBRE    datos de la empresa que factura
//	SELLO_CERT_P12 / SELLO_CERT_PASS  certificado cualificado (sin él, remisor simulado)
//	SELLO_AEAT                  pruebas|produccion (defecto pruebas)
//	FACTURABLE_API_URL          API pública de versiones (defecto https://api.facturable.es)
//
// On first run without SELLO_NIF (and without etc/config.json, the wizard's
// persisted identity) the agent does NOT abort: it starts in onboarding mode
// — GET /status answers {"onboarding":true} and PUT /v1/onboarding collects
// the emisor identity, validates it and stores it for the next start.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"facturable/internal/licenses"
	"facturable/internal/sello/declaration"
	"facturable/internal/sello/pdf"
	"facturable/internal/verifactu/domain/qr"

	"facturable/internal/sello"
	"facturable/internal/sello/ingest"
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/infrastructure/soap"

	sslmatep12 "software.sslmate.com/src/go-pkcs12"
)

func main() {
	version := env("FACTURABLE_VERSION", "0.1.0-unstable")
	db := env("SELLO_DB", "var/sello.sqlite")
	produccion := os.Getenv("SELLO_AEAT") == "produccion"

	prodName := env("SELLO_PRODUCER_NAME", "Facturable")
	prodNIF := env("SELLO_PRODUCER_NIF", "B00000000")
	prodContact := env("SELLO_PRODUCER_CONTACT", "soporte@facturable.es")
	if produccion && (prodNIF == "B00000000" || prodName == "Facturable") {
		log.Fatal("SELLO_PRODUCER_NIF/NAME/CONTACT reales obligatorios para remitir a producción (declaración responsable, art. 13-15)")
	}
	// Emisor identity: etc/config.json (persisted by the onboarding wizard)
	// provides the defaults, environment variables override them. Without
	// any identity the agent starts anyway in onboarding mode.
	configPath := env("SELLO_CONFIG", "etc/config.json")
	cfg := loadEmisorConfig(configPath)
	nif := env("SELLO_NIF", cfg.EmisorNIF)
	nombre := env("SELLO_NOMBRE", cfg.EmisorNombre)
	if nombre == "" {
		nombre = nif
	}
	// Telemetría opt-in (desactivada por defecto): la preferencia vive en
	// etc/config.json y este flag compartido la mantiene viva en memoria,
	// así el endpoint la cambia sin reiniciar y el ticker diario la lee.
	var telemOptIn atomic.Bool
	telemOptIn.Store(cfg.TelemetryOptIn)

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
		Store: store, Submitter: remisor,
		Flujo:     application.NewControlFlujo(time.Now),
		EmisorNIF: nif, EmisorNombre: nombre,
		LicenseCheck: licencia,
		Sistema: application.SystemInfo{
			NombreProductor: prodName, NIFProductor: prodNIF, // productor real: T-8.1
			NombreSistema: "Facturable Sello", IDSistema: "FS",
			Version: version, NumeroInstalacion: installationNumber(nif),
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

	// Carpeta vigilada (v1.1): deja un CSV en var/inbox y se sella solo.
	inbox := filepath.Join(filepath.Dir(db), "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		log.Printf("inbox: %v", err)
	}
	procesados := filepath.Join(inbox, "procesados")
	_ = os.MkdirAll(procesados, 0o700)
	go func() {
		for range time.Tick(15 * time.Second) {
			entries, err := os.ReadDir(inbox)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() || (!strings.HasSuffix(e.Name(), ".csv") && !strings.HasSuffix(e.Name(), ".CSV")) {
					continue
				}
				path := filepath.Join(inbox, e.Name())
				f, err := os.Open(path)
				if err != nil {
					continue
				}
				res, err := srv.IngestCSV(f)
				_ = f.Close()
				if err != nil {
					log.Printf("inbox %s: %v", e.Name(), err)
					continue
				}
				log.Printf("inbox %s: %d sellada(s), %d error(es)", e.Name(), res.Sellados, len(res.Errores))
				_ = os.Rename(path, filepath.Join(procesados, e.Name()))
			}
		}
	}()

	// Planificador de remisión: la AEAT exige esperar entre envíos; un ciclo
	// por minuto encaja con el control de flujo (spec AEAT §6.4.4.1).
	// Con relay configurado, el mismo ciclo vacía el buzón cifrado.
	go func() {
		for range time.Tick(60 * time.Second) {
			n, err := srv.SubmitPending(context.Background())
			if err != nil {
				log.Printf("remisión: %v", err)
			} else if n > 0 {
				log.Printf("remisión: %d chainRow(s) enviados", n)
			}
			if relayCli != nil {
				if p, err := relayCli.Pull(context.Background()); err != nil {
					log.Printf("relay pull: %v", err)
				} else if p > 0 {
					log.Printf("relay: %d sobre(s) ingestado(s)", p)
				}
			}
		}
	}()

	// Auto-update: la promesa de la suscripción es que el agente sigue la
	// normativa sin que el usuario reinstale nada. Cada 24h se pregunta al
	// API si hay versión nueva; la descarga se verifica con SHA-256 y se
	// reemplaza el binario en caliente (respaldo .bak), aplicándose al
	// reiniciar.
	updater := sello.NewUpdater(env("FACTURABLE_API_URL", "https://api.facturable.es"))
	go func() {
		check := func() {
			info, err := updater.CheckForUpdate(version)
			if err != nil {
				log.Printf("comprobación de actualizaciones: %v", err)
				return
			}
			if info == nil {
				return
			}
			log.Printf("Nueva versión disponible: %s (actual: %s)", info.Version, version)
			if err := updater.ApplyUpdate(info); err != nil {
				log.Printf("actualización: %v", err)
				return
			}
			log.Printf("Actualizado a %s. Reinicia para aplicar.", info.Version)
		}
		check()
		for range time.Tick(24 * time.Hour) {
			check()
		}
	}()

	// Telemetría opt-in: un ping anónimo al día junto al ciclo del updater,
	// solo si el usuario lo autorizó. El ping lleva versión, sistema
	// operativo y cuántas facturas se sellaron en los últimos 30 días —
	// NUNCA NIFs, nombres ni contenido de facturas: el recuento se computa
	// en local sobre el libro y solo el número cruza la red.
	telemetry := sello.NewTelemetry(env("FACTURABLE_API_URL", "https://api.facturable.es"))
	go func() {
		ping := func() {
			if !telemOptIn.Load() {
				return
			}
			count, err := store.CountRecent(nif, time.Now().Add(-30*24*time.Hour))
			if err != nil {
				log.Printf("telemetría: %v", err)
				return
			}
			if err := telemetry.Ping(context.Background(), version, runtime.GOOS, count); err != nil {
				log.Printf("telemetría: %v", err)
			}
		}
		ping()
		for range time.Tick(24 * time.Hour) {
			ping()
		}
	}()

	pin := os.Getenv("SELLO_PIN")
	// auth real en withPIN (middleware); guard queda como decorador neutro
	guard := func(next http.HandlerFunc) http.HandlerFunc { return next }
	certTries := newRateLimiter(5, time.Minute)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/certificates", func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if h, _, err := net.SplitHostPort(ip); err == nil {
			ip = h
		}
		if pin != "" && !certTries.allow(ip) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Too Many Requests", "status": 429, "detail": "demasiados intentos; espera un minuto"})
			return
		}
		saveCertificate(pin)(w, r)
	})
	mux.HandleFunc("POST /v1/emails", guard(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RecordUUID string `json:"record_uuid"`
			To         string `json:"to"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		f, ok, err := store.ByUUID(req.RecordUUID)
		if err != nil || !ok {
			writeErr(w, fmt.Errorf("no existe el registro solicitado"))
			return
		}
		cfg := sello.SMTPConfig{
			Host: env("SELLO_SMTP_HOST", ""), User: os.Getenv("SELLO_SMTP_USER"),
			Password: os.Getenv("SELLO_SMTP_PASS"), From: env("SELLO_SMTP_FROM", ""),
		}
		if cfg.Host == "" {
			writeErr(w, fmt.Errorf("SMTP no configurado (SELLO_SMTP_HOST)"))
			return
		}
		pdfBytes, err := renderRecordPDF(f, nif, nombre)
		if err != nil {
			writeErr(w, err)
			return
		}
		subject := fmt.Sprintf("Factura %s — VERI*FACTU", f.Serie)
		if err := sello.SendInvoiceEmail(cfg, req.To, subject, pdfBytes, f.Serie+".pdf"); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"enviado": true, "to": req.To})
	}))
	// POST /v1/stamps: sella el PDF ORIGINAL del cliente (post-procesador).
	// El programa del cliente genera la factura con 100 líneas → sube el PDF
	// aquí → extraemos los 9 campos fiscales → sellamos SU documento con QR.
	mux.HandleFunc("POST /v1/stamps", guard(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeErr(w, fmt.Errorf("no se pudo leer el formulario: %v", err))
			return
		}
		f, _, err := r.FormFile("invoice")
		if err != nil {
			writeErr(w, fmt.Errorf("falta el fichero 'invoice' (PDF de tu programa)"))
			return
		}
		defer f.Close()
		pdfBytes, err := io.ReadAll(io.LimitReader(f, 30<<20))
		if err != nil {
			writeErr(w, err)
			return
		}

		// campos fiscales confirmados por el humano (o auto-extraídos)
		q := sello.QuickInvoice{
			ClientName: r.FormValue("cliente"),
			ClientNIF:  r.FormValue("nif"),
			Concept:    r.FormValue("concepto"),
			Base:       r.FormValue("base"),
			VATRate:    r.FormValue("iva"),
		}
		if q.Concept == "" {
			q.Concept = "Factura sello directo"
		}
		if q.Base == "" {
			writeErr(w, fmt.Errorf("falta la base imponible total (campo 'base')"))
			return
		}

		// sellar
		lastSerie, _, _, _, _, _, okL, _ := store.Last(nif)
		if !okL {
			lastSerie = ""
		}
		draft, err := q.ToDraft(lastSerie, time.Now())
		if err != nil {
			writeErr(w, err)
			return
		}
		res, err := srv.IngestDrafts([]ingest.Draft{draft})
		if err != nil || res.Sellados == 0 {
			detalle := "no se pudo sellar"
			if res != nil && len(res.Errores) > 0 {
				detalle = res.Errores[0].Detalle
			}
			writeErr(w, fmt.Errorf("%s", detalle))
			return
		}

		// generar URL de cotejo y estampar el PDF ORIGINAL
		f2, _, _ := store.ByUUID(latestUUID(srv))
		urlQR, err := qr.VerificationURL(qrEnv(), qr.Query{
			NIF: nif, InvoiceNumber: draft.Series,
			ExpeditionDate: draft.Date, TotalAmount: draft.Breakdowns[0].Base,
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		stamped, err := pdf.Stamp(pdf.StampRequest{
			OriginalPDF: pdfBytes, Serie: draft.Series, Fecha: draft.Date,
			ClienteNIF: q.ClientNIF, ClienteNombre: q.ClientName,
			BaseImponible: q.Base, CuotaIVA: draft.Breakdowns[0].Fee,
			TipoIVA: q.VATRate, URLCotejo: urlQR,
			CSV: f2.CSV, Estado: f2.Estado,
		})
		if err != nil {
			writeErr(w, fmt.Errorf("no se pudo estampar tu PDF: %v", err))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-VERIFACTU.pdf", draft.Series))
		_, _ = w.Write(stamped)
	}))

	// Quick form (bloqueante MVP): 5 fields, agent fills all defaults.
	mux.HandleFunc("POST /v1/invoices", guard(func(w http.ResponseWriter, r *http.Request) {
		var q sello.QuickInvoice
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q); err != nil {
			writeErr(w, err)
			return
		}
		lastSerie, _, _, _, _, _, okL, err := store.Last(nif)
		if err != nil {
			writeErr(w, err)
			return
		}
		if !okL {
			lastSerie = ""
		}
		draft, err := q.ToDraft(lastSerie, time.Now())
		if err != nil {
			writeErr(w, err)
			return
		}
		res, err := srv.IngestDrafts([]ingest.Draft{draft})
		if err != nil {
			writeErr(w, err)
			return
		}
		if res.Sellados == 0 && len(res.Errores) > 0 {
			writeErr(w, fmt.Errorf("%s", res.Errores[0].Detalle))
			return
		}
		writeJSON(w, map[string]any{
			"sellada": true, "serie": draft.Series,
			"cuota": draft.Breakdowns[0].Fee, "total": draft.Breakdowns[0].Base,
		})
	}))

	// POST /v1/invoices/{uuid}:duplicate (Google-style custom method): re-
	// stamps a copy of that record dated today with a fresh series — the
	// "repeat last month's invoice" button. ServeMux wildcards must fill a
	// whole segment, so the ":duplicate" suffix arrives inside {uuid}.
	mux.HandleFunc("POST /v1/invoices/{uuid}", guard(func(w http.ResponseWriter, r *http.Request) {
		const action = ":duplicate"
		seg := r.PathValue("uuid")
		if !strings.HasSuffix(seg, action) {
			writeProblem(w, http.StatusNotFound, "no existe el recurso solicitado")
			return
		}
		res, err := srv.DuplicateInvoice(strings.TrimSuffix(seg, action))
		if err != nil {
			if errors.Is(err, sello.ErrRecordNotFound) {
				writeProblem(w, http.StatusNotFound, "no existe el registro solicitado")
				return
			}
			writeErr(w, err)
			return
		}
		writeJSON(w, res)
	}))

	// POST /v1/recurring: the fixed monthly list of frequent clients; every
	// invoice is generated + stamped in sequence and the summary answers.
	mux.HandleFunc("POST /v1/recurring", guard(func(w http.ResponseWriter, r *http.Request) {
		var clientes []sello.RecurringClient
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&clientes); err != nil {
			writeErr(w, fmt.Errorf("el cuerpo debe ser un array de facturas fijas (cliente, nif, concepto, base, iva): %v", err))
			return
		}
		if len(clientes) == 0 {
			writeErr(w, fmt.Errorf("el lote de facturas fijas está vacío"))
			return
		}
		writeJSON(w, sello.NewRecurringBatch(srv, clientes).StampAll())
	}))

	// Embedded 1-screen UI: the invoice form the smallest business can use.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(formUI))
	})

	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"data": map[string]string{"version": version}})
	})
	// GET /v1/updates: live answer to "is my agent current?" — consults the
	// public API on demand and reports both sides of the comparison.
	mux.HandleFunc("GET /v1/updates", func(w http.ResponseWriter, _ *http.Request) {
		info, err := updater.CheckForUpdate(version)
		if err != nil {
			writeErr(w, err)
			return
		}
		latest, available := version, false
		if info != nil {
			latest, available = info.Version, true
		}
		writeJSON(w, map[string]any{"current": version, "latest": latest, "available": available})
	})
	mux.HandleFunc("GET /v1/connectivity", checkConnectivity)
	// POST /v1/telemetry/optin {enabled}: the privacy toggle used by the
	// UI footer (and reachable without the wizard); persists to
	// etc/config.json and applies live.
	mux.HandleFunc("POST /v1/telemetry/optin", telemetryOptIn(configPath, &telemOptIn))
	// GET /v1/telemetry/status {optin}: current opt-in state for the UI.
	mux.HandleFunc("GET /v1/telemetry/status", telemetryStatus(&telemOptIn))
	mux.HandleFunc("GET /v1/declaration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(declaration.Text(declaration.Info{
			ProducerName:    prodName,
			ProducerNIF:     prodNIF,
			ProducerContact: prodContact,
			SystemName:      "Facturable Sello", SystemID: "FS",
			Version: version, Installation: installationNumber(nif),
			OnlyVerifactu: true, MultiOT: false,
		})))
	})
	// POST /v1/extract: upload a PDF, get fiscal fields pre-extracted for
	// the user to confirm. Text-based PDFs parse directly; scanned PDFs
	// fall back to tesseract OCR if available.
	mux.HandleFunc("POST /v1/extract", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeErr(w, fmt.Errorf("no se pudo leer el formulario: %v", err))
			return
		}
		f, _, err := r.FormFile("invoice")
		if err != nil {
			writeErr(w, fmt.Errorf("falta el fichero 'invoice' (PDF)"))
			return
		}
		defer f.Close()
		pdfBytes, err := io.ReadAll(io.LimitReader(f, 30<<20))
		if err != nil {
			writeErr(w, err)
			return
		}

		// 1) try text extraction from PDF streams
		text, textErr := pdf.ExtractTextFromPDF(pdfBytes)
		method := "text"
		if textErr != nil {
			// 2) OCR fallback via tesseract
			method = "ocr"
			text, textErr = pdf.OCRViaTesseract(pdfBytes, "spa")
			if textErr != nil {
				writeErr(w, fmt.Errorf("no se pudo extraer texto (ni texto directo ni OCR): %v", textErr))
				return
			}
		}

		// 3) parse fiscal fields from the extracted text
		nif, base, cuota, serie, fecha, err := pdf.ExtractFiscalFromText(text)
		if err != nil {
			// return what we got even if partial
			writeJSON(w, map[string]any{
				"method": method,
				"texto":  text[:min(2000, len(text))],
				"campos": map[string]string{
					"nif": nif, "base": base, "cuota": cuota,
					"serie": serie, "fecha": fecha,
				},
				"completo": false,
				"nota":     "extracción parcial: confirma los campos manualmente",
			})
			return
		}
		writeJSON(w, map[string]any{
			"method": method,
			"campos": map[string]string{
				"nif": nif, "base": base, "cuota": cuota,
				"serie": serie, "fecha": fecha,
			},
			"completo": true,
		})
	})

	// PUT /v1/onboarding: first-run wizard — validates the NIF, configures
	// the running service and persists the identity to etc/config.json so
	// the next start boots ready to stamp.
	mux.HandleFunc("PUT /v1/onboarding", onboardEmisor(srv, configPath, &nif, &nombre))
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"status": "ok", "app": "facturable-sello", "modo": modo,
			"onboarding": nif == ""}) // true = the wizard has not run yet
	})
	// Idempotent creation with client UUID (Zalando exception 1): same
	// uuid+body replays the stored result, different body conflicts 409.
	mux.HandleFunc("PUT /v1/records/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		if ok, motivo := licencia(); !ok {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title": "licencia", "status": 402, "detail": motivo})
			return
		}
		batchUUID := r.PathValue("uuid")
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			writeErr(w, err)
			return
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(body))
		if prev, ok, _ := store.GetIngestBatch(batchUUID); ok {
			if prev.BodyHash != hash {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"title": "Conflict", "status": 409,
					"detail": "ese UUID ya se usó con otro cuerpo", "instance": r.URL.Path})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(prev.Response))
			return
		}
		res, err := srv.IngestCSV(bytes.NewReader(body))
		if err != nil {
			writeErr(w, err)
			return
		}
		out, _ := json.Marshal(res)
		prev, fresh, err := store.SaveIngestBatch(persistence.IngestBatch{
			UUID: batchUUID, BodyHash: hash, Response: string(out)})
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !fresh && prev != nil && prev.BodyHash == hash {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(prev.Response))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(out)
	})
	mux.HandleFunc("POST /v1/submissions", func(w http.ResponseWriter, _ *http.Request) {
		n, err := srv.SubmitPending(context.Background())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]int{"enviados": n})
	})
	// POST /v1/cancellations: registers the cancellation of an invoice; the
	// cancellation record enters the chain like any other record.
	mux.HandleFunc("POST /v1/cancellations", guard(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Serie string `json:"serie"`
			Fecha string `json:"fecha"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		if req.Serie == "" || req.Fecha == "" {
			writeErr(w, fmt.Errorf("serie y fecha son obligatorios"))
			return
		}
		if _, err := srv.Cancel(req.Serie, req.Fecha); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"anulada": true, "serie": req.Serie, "fecha": req.Fecha})
	}))
	mux.HandleFunc("GET /v1/records/{uuid}/pdf", guard(func(w http.ResponseWriter, r *http.Request) {
		f, ok, err := store.ByUUID(r.PathValue("uuid"))
		if err != nil || !ok {
			writeErr(w, fmt.Errorf("no existe el registro solicitado"))
			return
		}
		b, err := renderRecordPDF(f, nif, nombre)
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(b)
	}))

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

	// Memoria de clientes: últimos clientes por frecuencia de uso, para
	// auto-rellenar el formulario (limit, defecto 20).
	mux.HandleFunc("GET /v1/clients", func(w http.ResponseWriter, r *http.Request) {
		limite := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				writeErr(w, fmt.Errorf("el parámetro 'limit' debe ser un entero positivo"))
				return
			}
			limite = n
		}
		clientes, err := store.FrequentClients(limite)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"data": clientes})
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

// installationNumber derives a stable per-machine installation id from
// hostname+NIF (audit: the literal "00001" was not unique per install).
func installationNumber(nif string) string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	sum := sha256.Sum256([]byte(host + "|" + nif))
	const hexDigits = "0123456789ABCDEF"
	out := make([]byte, 5)
	for i := range out {
		out[i] = hexDigits[int(sum[i])%16]
	}
	return string(out)
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
func construirRemisor() (application.Submitter, string) {
	p12 := os.Getenv("SELLO_CERT_P12")
	if p12 == "" {
		return fakeSubmitter{}, "simulado (sin certificado)"
	}
	cert, err := soap.LoadP12(p12, os.Getenv("SELLO_CERT_PASS"))
	if err != nil {
		log.Fatalf("certificado: %v", err)
	}
	produccion := os.Getenv("SELLO_AEAT") == "produccion"
	url := soap.Testing.PersonURL
	if produccion {
		url = soap.Production.PersonURL
	}
	if os.Getenv("SELLO_CERT_TIPO") == "sello" { // seal certs hit www10
		if produccion {
			url = soap.Production.SealURL
		} else {
			url = soap.Testing.SealURL
		}
	}
	return soap.NewClient(url, cert), "aeat"
}

// fakeSubmitter acepta cualquier XML devolviendo un veredicto correcto:
// permite probar el circuito completo sin certificado ni red.
type fakeSubmitter struct{}

func (fakeSubmitter) Submit(_ context.Context, soapXML string) (application.RespuestaLote, error) {
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// latestUUID returns the most recent record UUID.
func latestUUID(srv *sello.Service) string {
	filas, err := srv.Store.Ledger(srv.EmisorNIF, 0, 1_000_000, 1)
	if err != nil || len(filas) == 0 {
		return ""
	}
	return filas[0].UUID
}

// qrEnv selects the QR cotejo environment per SELLO_AEAT.
func qrEnv() qr.Environment {
	if os.Getenv("SELLO_AEAT") == "produccion" {
		return qr.Production
	}
	return qr.Testing
}

// renderRecordPDF builds the stamped PDF for a ledger row.
func renderRecordPDF(f persistence.LedgerRow, nif, nombre string) ([]byte, error) {
	urlQR, err := qr.VerificationURL(qrEnv(), qr.Query{
		NIF: f.NIF, InvoiceNumber: f.Serie, ExpeditionDate: f.Date, TotalAmount: f.Importe,
	})
	if err != nil {
		return nil, err
	}
	return pdf.Factura(pdf.DatosFactura{
		EmpresaNombre: nombre, EmpresaNIF: nif,
		Serie: f.Serie, Date: f.Date, Tipo: f.Tipo,
		ClienteNIF: "",
		Desgloses:  []pdf.DesgloseFactura{{Tipo: "", Base: baseDesde(f.Importe, f.Cuota), Cuota: f.Cuota}},
		CuotaTotal: f.Cuota, ImporteTotal: f.Importe,
		EstadoAEAT: f.Estado, CSV: f.CSV, URLCotejo: urlQR,
	})
}

// baseDesde computa base = total - cuota with exact cents (audit fix).
func baseDesde(importe, cuota string) string {
	ti, err1 := application.ToCents(importe)
	ci, err2 := application.ToCents(cuota)
	if err1 != nil || err2 != nil {
		return importe
	}
	return application.FromCents(ti - ci)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title": "Error del agente", "status": 400, "detail": err.Error()})
}

// writeProblem answers with a problem+json body and the given status
// (problem+json per restful-api-guidelines; writeErr keeps its 400 shortcut).
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title": http.StatusText(status), "status": status, "detail": detail})
}

// onboarding de certificado -------------------------------------------------

// saveCertificate recibe un P12 por multipart y lo instala (0600) para
// el próximo arranque. Contraseña por formulario aparte: nunca se guarda.
func saveCertificate(pin string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
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
		dest := filepath.Join("var/certificados", "empresa.p12.enc")
		if err := persistence.WriteSecret(dest, raw, pin); err != nil {
			writeErr(w, fmt.Errorf("no se pudo cifrar el certificado: %v", err))
			return
		}
		writeJSON(w, map[string]any{
			"guardado": dest,
			"nota":     "reinicia el agente con SELLO_CERT_P12=" + dest + " para remitir a la AEAT",
		})
	}
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
