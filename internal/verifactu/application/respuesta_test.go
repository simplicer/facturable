package application

import (
	"testing"
	"time"
)

const respuestaEjemplo = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"
 xmlns:sf="https://www2.agenciatributaria.gob.es/static_files/common/internet/dep/aplicaciones/es/aeat/tike/cont/ws/RespuestaSuministro.xsd">
  <soapenv:Body>
    <sf:RespuestaRegFactuSistemaFacturacion>
      <sf:Cabecera>
        <sf:IDEmisorFactura>B72877814</sf:IDEmisorFactura>
      </sf:Cabecera>
      <sf:CSV>ABC123DEF456GH78</sf:CSV>
      <sf:TiempoEsperaEnvio>60</sf:TiempoEsperaEnvio>
      <sf:EstadoEnvio>ParcialmenteCorrecto</sf:EstadoEnvio>
      <sf:RespuestaLinea>
        <sf:IDFactura>
          <sf:IDEmisorFactura>B72877814</sf:IDEmisorFactura>
          <sf:NumSerieFactura>F-2027-0001</sf:NumSerieFactura>
          <sf:FechaExpedicionFactura>05-01-2027</sf:FechaExpedicionFactura>
        </sf:IDFactura>
        <sf:TipoOperacion>Alta</sf:TipoOperacion>
        <sf:EstadoRegistro>Correcto</sf:EstadoRegistro>
        <sf:CodigoEstadoRegistro>0</sf:CodigoEstadoRegistro>
      </sf:RespuestaLinea>
      <sf:RespuestaLinea>
        <sf:IDFactura>
          <sf:IDEmisorFactura>B72877814</sf:IDEmisorFactura>
          <sf:NumSerieFactura>F-2027-0002</sf:NumSerieFactura>
          <sf:FechaExpedicionFactura>05-01-2027</sf:FechaExpedicionFactura>
        </sf:IDFactura>
        <sf:TipoOperacion>Alta</sf:TipoOperacion>
        <sf:EstadoRegistro>Incorrecto</sf:EstadoRegistro>
        <sf:CodigoEstadoRegistro>1207</sf:CodigoEstadoRegistro>
        <sf:DescripcionCodigoRegistro>NIF del destinatario no valido</sf:DescripcionCodigoRegistro>
      </sf:RespuestaLinea>
    </sf:RespuestaRegFactuSistemaFacturacion>
  </soapenv:Body>
</soapenv:Envelope>`

func TestParseRespuesta(t *testing.T) {
	t.Parallel()
	resp, err := ParseRespuesta(respuestaEjemplo)
	if err != nil {
		t.Fatalf("ParseRespuesta() = %v", err)
	}
	if resp.EstadoGlobal != "ParcialmenteCorrecto" {
		t.Fatalf("EstadoGlobal = %q", resp.EstadoGlobal)
	}
	if resp.CSV != "ABC123DEF456GH78" {
		t.Fatalf("CSV = %q", resp.CSV)
	}
	if resp.TiempoEsperaSeg != 60 {
		t.Fatalf("TiempoEsperaSeg = %d", resp.TiempoEsperaSeg)
	}
	if len(resp.Lineas) != 2 {
		t.Fatalf("líneas = %d, want 2", len(resp.Lineas))
	}
	if !resp.Lineas[0].Aceptada() {
		t.Fatalf("línea 0 debería estar aceptada: %+v", resp.Lineas[0])
	}
	if resp.Lineas[1].Aceptada() || resp.Lineas[1].CodigoError != "1207" {
		t.Fatalf("línea 1 = %+v", resp.Lineas[1])
	}
}

func TestParseRespuestaBasura(t *testing.T) {
	t.Parallel()
	if _, err := ParseRespuesta("no es xml"); err == nil {
		t.Fatal("ParseRespuesta(basura) = nil error")
	}
	if _, err := ParseRespuesta("<soapenv:Envelope><soapenv:Body/></soapenv:Envelope>"); err == nil {
		t.Fatal("ParseRespuesta(vacío) = nil error")
	}
}

func TestControlFlujo(t *testing.T) {
	t.Parallel()
	base := time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)
	now := base
	cf := NewControlFlujo(func() time.Time { return now })

	if !cf.PuedeEnviar(1) {
		t.Fatal("el primer envío debe poder salir inmediatamente")
	}
	cf.RegistrarEnvio(RespuestaLote{TiempoEsperaSeg: 60})

	if cf.PuedeEnviar(5) {
		t.Fatal("no debe poder enviar antes de que pasen los 60 segundos")
	}
	now = base.Add(59 * time.Second)
	if cf.PuedeEnviar(5) {
		t.Fatal("a los 59 segundos todavía no")
	}
	now = base.Add(60 * time.Second)
	if !cf.PuedeEnviar(5) {
		t.Fatal("a los 60 segundos sí")
	}

	now = base // reset: acumulados
	if !cf.PuedeEnviar(1000) {
		t.Fatal("con 1000 registros acumulados debe poder enviar aunque no haya pasado la espera")
	}

	cf.RegistrarEnvio(RespuestaLote{TiempoEsperaSeg: 120})
	now = base.Add(100 * time.Second)
	if cf.PuedeEnviar(5) {
		t.Fatal("la AEAT subió la espera a 120s: a los 100s no debe poder enviar")
	}
	now = base.Add(120 * time.Second)
	if !cf.PuedeEnviar(5) {
		t.Fatal("a los 120s sí")
	}
}
