package xmlalta

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// The golden files mirror the official message examples of the AEAT web
// services specification v1.0.3 (section 9.1.1 alta, section 9.2.1
// anulacion), parametrized with the exact values of the examples. The
// generated XML must be semantically identical to them.

func TestSubmissionXMLMatchesOfficialAltaExample(t *testing.T) {
	got, err := officialAltaSubmission().XML()
	if err != nil {
		t.Fatalf("XML() error = %v", err)
	}
	assertSemanticallyEqualFile(t, got, "testdata/alta-spec-9.1.1.xml")
}

func TestSubmissionXMLMatchesOfficialCancellationExample(t *testing.T) {
	got, err := officialCancellationSubmission().XML()
	if err != nil {
		t.Fatalf("XML() error = %v", err)
	}
	assertSemanticallyEqualFile(t, got, "testdata/anulacion-spec-9.2.1.xml")
}

// officialAltaSubmission builds the submission of the official alta example
// (spec v1.0.3, section 9.1.1, pages 53-55) with its exact values.
func officialAltaSubmission() Submission {
	alta := NewRegistrationRecord()
	alta.InvoiceID = InvoiceID{
		IssuerID:       "AAAA",
		Number:         "12345",
		ExpeditionDate: "13-09-2024",
	}
	alta.IssuerName = "XXXXX"
	alta.InvoiceType = "F1"
	alta.OperationDescription = "Descripc"
	alta.Recipients = []Recipient{{Name: "YYYY", TaxID: "BBBB"}}
	alta.Breakdown = []BreakdownLine{
		{RegimeKey: "01", OperationQualification: "S1", TaxRate: "4", TaxBase: "10", TaxFee: "0.4"},
		{RegimeKey: "01", OperationQualification: "S1", TaxRate: "21", TaxBase: "100", TaxFee: "21"},
	}
	alta.TotalTax = "21.4"
	alta.TotalAmount = "131.4"
	alta.Chaining = &Chaining{PreviousRecord: &PreviousRecord{
		IssuerID:       "AAAA",
		Number:         "44",
		ExpeditionDate: "13-09-2024",
		Fingerprint:    "HuellaRegistroAnterior",
	}}
	alta.System = officialSoftwareSystem()
	alta.GeneratedAt = "2024-09-13T19:20:30+01:00"
	alta.Fingerprint = "Huella"
	return NewSubmission(Issuer{Name: "XXXXX", TaxID: "AAAA"}, alta)
}

// officialCancellationSubmission builds the submission of the official
// anulacion example (spec v1.0.3, section 9.2.1, page 66) with its values.
func officialCancellationSubmission() Submission {
	anulacion := NewCancellationRecord()
	anulacion.InvoiceID = CancelledInvoiceID{
		IssuerID:       "AAAA",
		Number:         "12345",
		ExpeditionDate: "13-09-2024",
	}
	anulacion.Chaining = &Chaining{PreviousRecord: &PreviousRecord{
		IssuerID:       "AAAA",
		Number:         "44",
		ExpeditionDate: "13-09-2024",
		Fingerprint:    "HuellaRegistroAnterior",
	}}
	anulacion.System = officialSoftwareSystem()
	anulacion.GeneratedAt = "2024-09-13T19:20:30+01:00"
	anulacion.Fingerprint = "Huella"
	return NewSubmission(Issuer{Name: "XXXXX", TaxID: "AAAA"}, anulacion)
}

func officialSoftwareSystem() SoftwareSystem {
	return SoftwareSystem{
		Name:                "SSSS",
		TaxID:               "NNNN",
		SystemName:          "NombreSistemaInformatico",
		SystemID:            "77",
		Version:             "1.0.03",
		InstallationNumber:  "383",
		VerifactuOnlyUse:    "N",
		MultiTaxpayerUse:    "S",
		MultipleOTIndicator: "S",
	}
}

func assertSemanticallyEqualFile(t *testing.T, gotXML, goldenPath string) {
	t.Helper()
	wantXML, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	got, err := parseXMLTree(gotXML)
	if err != nil {
		t.Fatalf("parsing generated XML: %v", err)
	}
	want, err := parseXMLTree(string(wantXML))
	if err != nil {
		t.Fatalf("parsing golden file %s: %v", goldenPath, err)
	}
	if diff := treeDiff(want, got); diff != "" {
		t.Fatalf("generated XML differs from golden %s:\n%s\n--- generated ---\n%s", goldenPath, diff, gotXML)
	}
}

// xmlNode is one element of a parsed document, namespace included.
type xmlNode struct {
	name  xml.Name
	attrs []xml.Attr
	text  string
	kids  []*xmlNode
}

func parseXMLTree(data string) (*xmlNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(data))
	decoder.Strict = true
	var stack []*xmlNode
	var root *xmlNode
	for {
		token, err := decoder.Token()
		if err != nil {
			if root != nil {
				return root, nil
			}
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			node := &xmlNode{name: t.Name, attrs: append([]xml.Attr(nil), t.Attr...)}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, node)
			} else if root == nil {
				root = node
			}
			stack = append(stack, node)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
			if len(stack) == 0 && root != nil {
				// Continue: trailing whitespace/procinst is ignored.
			}
		}
	}
}

// treeDiff returns "" when both documents are semantically identical: same
// element tree (namespace + local name), same attribute sets, same trimmed
// text, same child order.
func treeDiff(want, got *xmlNode) string {
	switch {
	case want == nil && got == nil:
		return ""
	case want == nil:
		return fmt.Sprintf("unexpected extra element <%s>", nodeLabel(got))
	case got == nil:
		return fmt.Sprintf("missing element <%s>", nodeLabel(want))
	}
	if want.name != got.name {
		return fmt.Sprintf("element name: want <%s>, got <%s>", nodeLabel(want), nodeLabel(got))
	}
	if diff := attrDiff(want, got); diff != "" {
		return diff
	}
	if strings.TrimSpace(want.text) != strings.TrimSpace(got.text) {
		return fmt.Sprintf("<%s> text: want %q, got %q", nodeLabel(want), strings.TrimSpace(want.text), strings.TrimSpace(got.text))
	}
	if len(want.kids) != len(got.kids) {
		return fmt.Sprintf("<%s> children: want %d, got %d (%s)", nodeLabel(want), len(want.kids), len(got.kids), kidsSummary(got))
	}
	for i := range want.kids {
		if diff := treeDiff(want.kids[i], got.kids[i]); diff != "" {
			return diff
		}
	}
	return ""
}

func attrDiff(want, got *xmlNode) string {
	wantAttrs, gotAttrs := attrSet(want), attrSet(got)
	var problems []string
	for k, v := range wantAttrs {
		if gotAttrs[k] != v {
			if _, present := gotAttrs[k]; !present {
				problems = append(problems, fmt.Sprintf("missing attribute %s=%q", k, v))
			} else {
				problems = append(problems, fmt.Sprintf("attribute %s: want %q, got %q", k, v, gotAttrs[k]))
			}
		}
	}
	for k, v := range gotAttrs {
		if _, present := wantAttrs[k]; !present {
			problems = append(problems, fmt.Sprintf("unexpected attribute %s=%q", k, v))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Sprintf("<%s> attributes: %s", nodeLabel(want), strings.Join(problems, "; "))
	}
	return ""
}

func attrSet(n *xmlNode) map[string]string {
	set := make(map[string]string, len(n.attrs))
	for _, a := range n.attrs {
		key := a.Name.Local
		if a.Name.Space != "" {
			key = a.Name.Space + ":" + a.Name.Local
		}
		set[key] = a.Value
	}
	return set
}

func nodeLabel(n *xmlNode) string {
	if n.name.Space == "" {
		return n.name.Local
	}
	return n.name.Space + ":" + n.name.Local
}

func kidsSummary(n *xmlNode) string {
	labels := make([]string, 0, len(n.kids))
	for _, kid := range n.kids {
		labels = append(labels, nodeLabel(kid))
	}
	return strings.Join(labels, ", ")
}
