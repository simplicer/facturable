package persistence

import (
	"strings"
	"testing"
)

// wantClients compara la memoria de clientes con la lista esperada.
func wantClients(t *testing.T, got, want []FrequentClient) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("FrequentClients = %+v (want %d clients: %+v)", got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FrequentClients[%d] = %+v (want %+v)", i, got[i], want[i])
		}
	}
}

func TestUpsertClient(t *testing.T) {
	tests := []struct {
		name     string
		nif      string
		nombre   string
		wantRows int
	}{
		{"empty nif is not remembered", "", "ACME SL", 0},
		{"empty name is not remembered", "B44531218", "", 0},
		{"valid client is remembered", "B44531218", "ACME SL", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			if err := s.UpsertClient(tt.nif, tt.nombre); err != nil {
				t.Fatalf("UpsertClient(%q, %q): %v", tt.nif, tt.nombre, err)
			}
			got, err := s.FrequentClients(10)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.wantRows {
				t.Fatalf("FrequentClients = %+v (want %d rows)", got, tt.wantRows)
			}
		})
	}
}

func TestUpsertClientRefreshesNameKeepsOneRow(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertClient("B44531218", "ACME SL"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertClient("B44531218", "ACME SL S.A."); err != nil {
		t.Fatal(err)
	}
	got, err := s.FrequentClients(10)
	if err != nil {
		t.Fatal(err)
	}
	wantClients(t, got, []FrequentClient{{NIF: "B44531218", Nombre: "ACME SL S.A.", Usos: 0}})
}

// El escenario de producto: tres registros sellados con dos clientes
// distintos; la memoria debe quedar ordenada por frecuencia de uso.
func TestFrequentClientsOrdersByRecordCount(t *testing.T) {
	s := newTestStore(t)

	visits := []struct {
		nif, nombre string
		row         StoredRecord
	}{
		{"B44531218", "ACME SL", chainRow(1, strings.Repeat("A", 64), "")},
		{"12345678Z", "Juan Pérez", chainRow(2, strings.Repeat("B", 64), "")},
		{"B44531218", "ACME SL", chainRow(3, strings.Repeat("C", 64), "")},
	}
	for _, v := range visits {
		v.row.ClientNIF = v.nif
		if _, err := s.Append(v.row); err != nil {
			t.Fatalf("Append(%s): %v", v.row.Serie, err)
		}
		if err := s.UpsertClient(v.nif, v.nombre); err != nil {
			t.Fatalf("UpsertClient(%s): %v", v.nif, err)
		}
	}

	got, err := s.FrequentClients(10)
	if err != nil {
		t.Fatal(err)
	}
	wantClients(t, got, []FrequentClient{
		{NIF: "B44531218", Nombre: "ACME SL", Usos: 2},
		{NIF: "12345678Z", Nombre: "Juan Pérez", Usos: 1},
	})

	limited, err := s.FrequentClients(1)
	if err != nil {
		t.Fatal(err)
	}
	wantClients(t, limited, []FrequentClient{{NIF: "B44531218", Nombre: "ACME SL", Usos: 2}})
}
