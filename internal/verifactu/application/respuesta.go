// Package application holds the Veri*Factu use cases: parsing AEAT
// submission responses, the mandatory flow control between batches and the
// Submitter port the infrastructure implements (SOAP client).
package application

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RespuestaLote is the decoded AEAT answer to one submission batch.
type RespuestaLote struct {
	// EstadoGlobal: Correcto | ParcialmenteCorrecto | Incorrecto.
	EstadoGlobal string
	// CSV is the 16-character code proving the batch was received. It MUST
	// be persisted immediately: it cannot be recovered later (spec v1.0.3).
	CSV string
	// TiempoEsperaSeg is the updated wait in seconds between submissions.
	TiempoEsperaSeg int
	// Lineas holds the per-record verdicts.
	Lineas []RespuestaLinea
}

// RespuestaLinea is the verdict for one record inside a batch.
type RespuestaLinea struct {
	NIFEmisor     string
	NumSerie      string
	Date          string
	TipoOperacion string // Alta | Anulacion
	// Estado: Correcto | AceptadoConErrores | Incorrecto.
	Estado         string
	CodigoError    string
	DescripcionRaw string
}

// Aceptada reports whether the record counts as registered at the AEAT.
func (l RespuestaLinea) Aceptada() bool {
	return l.Estado == "Correcto" || l.Estado == "AceptadoConErrores"
}

// Submitter is the port that ships a signed SOAP batch to the AEAT and
// returns its answer. Implementations live in infrastructure.
type Submitter interface {
	Submit(ctx context.Context, soapXML string) (RespuestaLote, error)
}

// respuestaEnvelope mirrors the AEAT response XML by local name (prefixes
// vary: sf:, sum:, ...). Field names follow the record design of the
// official web services description spec v1.0.3.
type respuestaEnvelope struct {
	Body struct {
		Respuesta struct {
			CSV             string `xml:"CSV"`
			TiempoEsperaRaw string `xml:"TiempoEsperaEnvio"`
			EstadoGlobal    string `xml:"EstadoEnvio"`
			Lineas          []struct {
				IDFactura struct {
					NIF   string `xml:"IDEmisorFactura"`
					Serie string `xml:"NumSerieFactura"`
					Date  string `xml:"FechaExpedicionFactura"`
				} `xml:"IDFactura"`
				Tipo        string `xml:"TipoOperacion"`
				Estado      string `xml:"EstadoRegistro"`
				CodigoRaw   string `xml:"CodigoEstadoRegistro"`
				Description string `xml:"DescripcionCodigoRegistro"`
			} `xml:"RespuestaLinea"`
		} `xml:"RespuestaRegFactuSistemaFacturacion"`
	} `xml:"Body"`
}

// ParseRespuesta decodes a raw AEAT SOAP response. It returns an error when
// the payload is not a recognizable response envelope; transport faults are
// the caller's business.
func ParseRespuesta(soapXML string) (RespuestaLote, error) {
	var env respuestaEnvelope
	if err := xml.Unmarshal([]byte(soapXML), &env); err != nil {
		return RespuestaLote{}, fmt.Errorf("la respuesta de la AEAT no se pudo leer: %w", err)
	}
	r := env.Body.Respuesta
	if r.EstadoGlobal == "" && r.CSV == "" && len(r.Lineas) == 0 {
		return RespuestaLote{}, fmt.Errorf("la respuesta de la AEAT no contiene datos reconocibles (¿fallo del servicio?)")
	}
	espera, err := strconv.Atoi(strings.TrimSpace(r.TiempoEsperaRaw))
	if err != nil {
		espera = 0
	}
	out := RespuestaLote{
		EstadoGlobal:    strings.TrimSpace(r.EstadoGlobal),
		CSV:             strings.TrimSpace(r.CSV),
		TiempoEsperaSeg: espera,
	}
	for _, l := range r.Lineas {
		out.Lineas = append(out.Lineas, RespuestaLinea{
			NIFEmisor:      strings.TrimSpace(l.IDFactura.NIF),
			NumSerie:       strings.TrimSpace(l.IDFactura.Serie),
			Date:           strings.TrimSpace(l.IDFactura.Date),
			TipoOperacion:  strings.TrimSpace(l.Tipo),
			Estado:         strings.TrimSpace(l.Estado),
			CodigoError:    strings.TrimSpace(l.CodigoRaw),
			DescripcionRaw: strings.TrimSpace(l.Description),
		})
	}
	return out, nil
}

// ControlFlujo implements the mandatory submission flow control (spec
// §6.4.4.1): wait TiempoEsperaEnvio seconds between batches (initially 60)
// OR until maxPendientes records are queued, whichever happens first.
type ControlFlujo struct {
	espera        time.Duration
	maxPendientes int
	ultimoEnvio   time.Time
	ahora         func() time.Time
}

// NewControlFlujo builds a controller with the official initial values:
// 60 seconds and 1000 records per batch.
func NewControlFlujo(ahora func() time.Time) *ControlFlujo {
	return &ControlFlujo{espera: 60 * time.Second, maxPendientes: 1000, ahora: ahora}
}

// PuedeEnviar reports whether a new batch may ship now.
func (c *ControlFlujo) PuedeEnviar(pendientes int) bool {
	if pendientes >= c.maxPendientes {
		return true
	}
	return !c.ultimoEnvio.IsZero() && c.ahora().Sub(c.ultimoEnvio) >= c.espera ||
		c.ultimoEnvio.IsZero()
}

// RegistrarEnvio records a shipment and adopts the fresh wait time the AEAT
// returned, when it returns one.
func (c *ControlFlujo) RegistrarEnvio(resp RespuestaLote) {
	c.ultimoEnvio = c.ahora()
	if resp.TiempoEsperaSeg > 0 {
		c.espera = time.Duration(resp.TiempoEsperaSeg) * time.Second
	}
}
