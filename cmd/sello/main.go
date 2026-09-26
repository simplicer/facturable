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
	"time"

	"facturable/internal/sello/pdf"
	"facturable/internal/verifactu/domain/qr"

	"facturable/internal/sello"
	"facturable/internal/sello/persistencia"
	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/infrastructure/soap"

	sslmatep12 "software.sslmate.com/src/go-pkcs12"
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
	mux.HandleFunc("PUT /v1/certificado", guardarCertificado)
	mux.HandleFunc("GET /v1/conexion-aeat", probarConexion)
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "app": "facturable-sello", "modo": modo})
	})
	mux.HandleFunc("POST /v1/registros", func(w http.ResponseWriter, r *http.Request) {
		res, err := srv.IngestaCSV(r.Body)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /v1/remisiones", func(w http.ResponseWriter, _ *http.Request) {
		n, err := srv.EnviarPendientes(context.Background())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]int{"enviados": n})
	})
	mux.HandleFunc("GET /v1/registros/{id}/pdf", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, fmt.Errorf("identificador de registro no válido"))
			return
		}
		f, ok, err := store.PorID(id)
		if err != nil || !ok {
			writeErr(w, fmt.Errorf("no existe el registro %d", id))
			return
		}
		urlQR, err := qr.VerificationURL(qr.Production, qr.Query{
			NIF: f.NIF, InvoiceNumber: f.Serie, ExpeditionDate: f.Fecha, TotalAmount: f.Importe,
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		b, err := pdf.Factura(pdf.DatosFactura{
			EmpresaNombre: nombre, EmpresaNIF: nif,
			Serie: f.Serie, Fecha: f.Fecha, Tipo: f.Tipo,
			ClienteNIF: "", Descripcion: "Registro de facturación Veri*Factu",
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
	mux.HandleFunc("GET /v1/registros", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("formato") == "csv" {
			desde, hasta := q.Get("desde"), q.Get("hasta")
			if desde == "" {
				desde = "01-01-2020"
			}
			if hasta == "" {
				hasta = time.Now().Format("02-01-2006")
			}
			b, _, err := srv.ExportarPeriodo(desde, hasta)
			if err != nil {
				writeErr(w, err)
				return
			}
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=libro-facturas.csv")
			_, _ = w.Write(b)
			return
		}
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

func (remisorSimulado) Remitir(_ context.Context, soapXML string) (application.RespuestaLote, error) {
	serie, fecha := serieYFechaDelXML(soapXML)
	return application.RespuestaLote{
		EstadoGlobal: "Correcto", CSV: "SIMULADO0000000A",
		Lineas: []application.RespuestaLinea{{
			NIFEmisor: os.Getenv("SELLO_NIF"), NumSerie: serie, Fecha: fecha,
			TipoOperacion: "Alta", Estado: "Correcto"}},
	}, nil
}

// serieYFechaDelXML extrae la primera IDFactura del sobre (sólo simulación).
func serieYFechaDelXML(xmlStr string) (serie, fecha string) {
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

// guardarCertificado recibe un P12 por multipart y lo instala (0600) para
// el próximo arranque. Contraseña por formulario aparte: nunca se guarda.
func guardarCertificado(w http.ResponseWriter, r *http.Request) {
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

// probarConexion verifica carga del certificado y handshake TLS contra el
// entorno de pruebas de la AEAT (prewww1). Diagnóstico de onboarding.
func probarConexion(w http.ResponseWriter, _ *http.Request) {
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
