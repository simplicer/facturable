// Package sello une la ingesta, el caso de uso SellarAlta, el libro SQLite
// y el Remisor AEAT: es el motor del agente local (el SIF).
package sello

import (
	"context"
	"fmt"
	"io"
	"time"

	"facturable/internal/sello/ingesta"
	"facturable/internal/sello/persistencia"
	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/domain"
)

// Servicio orquesta el ciclo completo del agente.
type Servicio struct {
	Store   *persistencia.Store
	Remisor application.Remisor
	Flujo   *application.ControlFlujo

	EmisorNIF, EmisorNombre string
	Sistema                 application.SistemaLocal
}

// ResultadoIngesta resume una ingesta de CSV.
type ResultadoIngesta struct {
	Sellados int                 `json:"sellados"`
	Errores  []ingesta.ErrorFila `json:"errores,omitempty"`
}

// IngestaCSV valida, sella y guarda un CSV de facturas.
func (s *Servicio) IngestaCSV(r io.Reader) (*ResultadoIngesta, error) {
	borradores, errores := ingesta.LeerCSV(r)
	res := &ResultadoIngesta{Errores: errores}

	for _, b := range borradores {
		estado, err := s.estadoCadena()
		if err != nil {
			return res, err
		}
		sellado, err := application.SellarAlta(s.EmisorNIF, s.EmisorNombre,
			application.BorradorAlta{
				NumeroSerie: b.NumeroSerie, Fecha: b.Fecha, Tipo: b.Tipo,
				Descripcion: b.Descripcion, NIFCliente: b.NIFCliente,
				NombreCliente: b.NombreCliente,
				Desgloses:     aApplicationDesgloses(b.Desgloses),
			}, s.Sistema, estado, time.Now())
		if err != nil {
			res.Errores = append(res.Errores, ingesta.ErrorFila{
				Fila: b.Fila, Campo: "factura", Detalle: err.Error()})
			continue
		}
		if _, err := s.Store.Guardar(persistencia.RegistroGuardado{
			NIF: sellado.NIF, Serie: sellado.Serie, Fecha: sellado.Fecha,
			Tipo: sellado.Tipo, Generado: sellado.Generado,
			Cuota: sellado.Cuota, Importe: sellado.Importe,
			Huella: sellado.Huella, XML: sellado.XML,
			EnlaceSerie: sellado.EnlaceSerie, EnlaceFecha: sellado.EnlaceFecha,
			EnlaceHuella: sellado.EnlaceHuella,
		}); err != nil {
			return res, fmt.Errorf("no se pudo guardar %s: %w", sellado.Serie, err)
		}
		res.Sellados++
	}
	return res, nil
}

// EnviarPendientes remite los registros pendientes respetando el control
// de flujo (espera 60 s o acumulado de lote) y registra cada respuesta.
func (s *Servicio) EnviarPendientes(ctx context.Context) (int, error) {
	pendientes, err := s.Store.Pendientes(s.EmisorNIF, 1000)
	if err != nil {
		return 0, err
	}
	if len(pendientes) == 0 {
		return 0, nil
	}
	// El control de flujo de la AEAT obliga a esperar TiempoEsperaEnvio
	// (60 s inicial) entre envíos: el agente envía un sobre por ciclo y su
	// planificador (60 s) vuelve a llamar. El acumulado a lote llegará con
	// el envío agrupado (véase PLAN-DESARROLLO T-2.5 mejora).
	if !s.Flujo.PuedeEnviar(len(pendientes)) {
		return 0, nil
	}
	p := pendientes[0]
	lote, err := s.Remisor.Remitir(ctx, p.XML)
	if err != nil {
		return 0, fmt.Errorf("envío de %s: %w", p.Serie, err)
	}
	s.Flujo.RegistrarEnvio(lote)
	if err := s.registrarRespuesta(p, lote); err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *Servicio) registrarRespuesta(p persistencia.Pendiente, lote application.RespuestaLote) error {
	linea := lineaDe(p, lote)
	if linea == nil {
		return s.Store.RegistrarRemision(persistencia.RemisionGuardada{
			RegistroID: p.ID, Estado: "rechazado",
			Detalle: "la AEAT no devolvió el veredicto de este registro"})
	}
	estado := map[string]string{
		"Correcto": "correcto", "AceptadoConErrores": "aceptado_errores", "Incorrecto": "rechazado",
	}[linea.Estado]
	if estado == "" {
		estado = "rechazado"
	}
	detalle := linea.DescripcionRaw
	if linea.CodigoError != "" && linea.Estado != "Correcto" {
		info := domain.ClasificarError(linea.CodigoError)
		detalle = fmt.Sprintf("%s — %s", info.Descripcion, info.Accion)
	}
	return s.Store.RegistrarRemision(persistencia.RemisionGuardada{
		RegistroID: p.ID, Estado: estado, CSV: lote.CSV,
		Codigo: linea.CodigoError, Detalle: detalle})
}

func lineaDe(p persistencia.Pendiente, lote application.RespuestaLote) *application.RespuestaLinea {
	for i := range lote.Lineas {
		l := &lote.Lineas[i]
		if l.NumSerie == p.Serie && l.Fecha == p.Fecha {
			return l
		}
	}
	return nil
}

func (s *Servicio) estadoCadena() (application.EstadoCadena, error) {
	serie, fecha, huella, enlS, enlF, enlH, ok, err := s.Store.Ultimo(s.EmisorNIF)
	if err != nil {
		return application.EstadoCadena{}, err
	}
	if !ok {
		return application.EstadoCadena{}, nil
	}
	estado := application.EstadoCadena{
		Ultimo: &application.EnlaceTriple{Serie: serie, Fecha: fecha, Huella: huella},
	}
	if enlS != "" {
		estado.DeclaradoPorUltimo = &application.EnlaceTriple{Serie: enlS, Fecha: enlF, Huella: enlH}
	}
	aS, aF, aH, aOk, err := s.Store.Anterior(s.EmisorNIF)
	if err != nil {
		return estado, err
	}
	if aOk {
		estado.Anterior = &application.EnlaceTriple{Serie: aS, Fecha: aF, Huella: aH}
	}
	return estado, nil
}

func aApplicationDesgloses(ds []ingesta.DesgloseBorrador) []application.DesgloseAlta {
	out := make([]application.DesgloseAlta, 0, len(ds))
	for _, d := range ds {
		out = append(out, application.DesgloseAlta{
			TipoImpositivo: d.TipoImpositivo, Base: d.Base, Cuota: d.Cuota})
	}
	return out
}

// ahoraReal es el reloj de producción del agente.
func ahoraReal() time.Time { return time.Now() }
