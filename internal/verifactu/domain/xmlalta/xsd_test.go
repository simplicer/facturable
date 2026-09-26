// T-7.1: validates a GENERATED envelope (realistic data) against the
// official AEAT XSDs. The spec examples use placeholder values (NIF "AAAA")
// that violate the schema facets, so the golden file is not suitable for
// schema validation; golden comparison lives in golden_test.go.
// Skipped when xmllint is not installed.
package xmlalta

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenContraXSDOficial(t *testing.T) {
	if _, err := exec.LookPath("xmllint"); err != nil {
		t.Skip("xmllint no disponible")
	}
	xsd, err := filepath.Abs(filepath.Join("testdata", "xsd", "SuministroLR.xsd"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(xsd); err != nil {
		t.Skipf("XSD oficial no descargado: %v", err)
	}

	// envelope generado con datos realistas
	rec := NewRegistrationRecord()
	rec.InvoiceID = InvoiceID{IssuerID: "B72877814", Number: "F-2027-0001", ExpeditionDate: "05-01-2027"}
	rec.IssuerName = "FACTURABLE TEST SL"
	rec.InvoiceType = "F1"
	rec.OperationDescription = "Validación XSD contra esquema oficial"
	rec.Recipients = []Recipient{{Name: "CLIENTE QA SL", TaxID: "B44531218"}}
	rec.Breakdown = []BreakdownLine{{
		Tax: "01", RegimeKey: "01", OperationQualification: "S1",
		TaxRate: "21", TaxBase: "10.00", TaxFee: "2.10",
	}}
	rec.TotalTax = "2.10"
	rec.TotalAmount = "12.10"
	rec.Chaining = &Chaining{FirstRecord: "S"}
	rec.System = SoftwareSystem{
		Name: "FACTURABLE", TaxID: "B72877814",
		SystemName: "Facturable Sello", SystemID: "FS", Version: "0.1.0",
		InstallationNumber: "00001", VerifactuOnlyUse: "S", MultiTaxpayerUse: "N", MultipleOTIndicator: "N",
	}
	rec.GeneratedAt = "2027-01-05T10:00:00+01:00"
	rec.Fingerprint = strings.Repeat("A", 64)
	if err := rec.Validate(); err != nil {
		t.Fatalf("registro de prueba inválido: %v", err)
	}
	sub := NewSubmission(Issuer{Name: "FACTURABLE TEST SL", TaxID: "B72877814"}, rec)
	sobre, err := sub.XML()
	if err != nil {
		t.Fatal(err)
	}

	inner, err := extractBodyBlock(sobre)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "body.xml")
	if err := os.WriteFile(tmp, []byte(inner), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("xmllint", "--noout", "--nonet", "--schema", xsd, tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nuestro XML no valida contra el XSD oficial: %v: %s", err, out)
	}
}

// extractBodyBlock aísla RegFactuSistemaFacturacion con los xmlns necesarios.
func extractBodyBlock(soap string) (string, error) {
	const open, close = "<sum:RegFactuSistemaFacturacion>", "</sum:RegFactuSistemaFacturacion>"
	i := indexOf(soap, open)
	if i < 0 {
		return "", os.ErrNotExist
	}
	j := indexOf(soap[i:], close)
	if j < 0 {
		return "", os.ErrNotExist
	}
	// reinyectar los xmlns en la raíz del bloque (el trozo hereda los del sobre)
	bloque := soap[i : i+j+len(close)]
	decl := `<sum:RegFactuSistemaFacturacion xmlns:sum="https://www2.agenciatributaria.gob.es/static_files/common/internet/dep/aplicaciones/es/aeat/tike/cont/ws/SuministroLR.xsd" xmlns:sum1="https://www2.agenciatributaria.gob.es/static_files/common/internet/dep/aplicaciones/es/aeat/tike/cont/ws/SuministroInformacion.xsd" xmlns:xd="http://www.w3.org/2000/09/xmldsig#">`
	cierre := "</sum:RegFactuSistemaFacturacion>"
	return decl + bloque[len(open):len(bloque)-len(cierre)] + cierre, nil
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
