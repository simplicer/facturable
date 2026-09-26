package licencias

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// nuevoAPIPrueba monta el servicio con base temporal y reloj inyectado.
func nuevoAPIPrueba(t *testing.T, ahora func() time.Time) (*API, *httptest.Server) {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/lic.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	priv, _, err := CargarOGenerarClave(t.TempDir() + "/k.pem")
	if err != nil {
		t.Fatal(err)
	}
	api, err := NuevaAPI(db, priv)
	if err != nil {
		t.Fatal(err)
	}
	api.ahora = ahora
	mux := http.NewServeMux()
	api.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return api, srv
}

func put(t *testing.T, srv *httptest.Server, id, cuerpo string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/licenses/"+id, strings.NewReader(cuerpo))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestLicenciasPUTIdempotente(t *testing.T) {
	_, srv := nuevoAPIPrueba(t, time.Now)
	cuerpo := `{"plan":"pro","nifs":["B72877814"],"dias_validez":31,"grace_days":7}`

	r1 := put(t, srv, "11111111-1111-1111-1111-111111111111", cuerpo)
	if r1.StatusCode != 201 {
		t.Fatalf("primer PUT = %d", r1.StatusCode)
	}
	resp1 := put(t, srv, "22222222-2222-2222-2222-222222222222", cuerpo)
	var env1 map[string]map[string]any
	_ = json.NewDecoder(resp1.Body).Decode(&env1)
	_ = env1["data"]["token"].(string)

	r2 := put(t, srv, "11111111-1111-1111-1111-111111111111", cuerpo)
	if r2.StatusCode != 200 {
		t.Fatalf("replay PUT = %d, want 200", r2.StatusCode)
	}
	r3 := put(t, srv, "11111111-1111-1111-1111-111111111111", `{"plan":"basico","nifs":[],"dias_validez":31,"grace_days":7}`)
	if r3.StatusCode != 409 {
		t.Fatalf("PUT conflictivo = %d, want 409", r3.StatusCode)
	}
}

func TestLicenciasCheckReglaNuncaRehen(t *testing.T) {
	ahora := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	api, srv := nuevoAPIPrueba(t, func() time.Time { return ahora })
	_ = api
	r := put(t, srv, "33333333-3333-3333-3333-333333333333", `{"plan":"pro","nifs":[],"dias_validez":30,"grace_days":7}`)
	if r.StatusCode != 201 {
		t.Fatalf("PUT = %d", r.StatusCode)
	}

	get := func() map[string]any {
		resp, err := srv.Client().Get(srv.URL + "/v1/licenses/33333333-3333-3333-3333-333333333333/validity")
		if err != nil {
			t.Fatal(err)
		}
		var env map[string]map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&env)
		return env["data"]
	}

	d := get()
	if d["stamp_new"] != true || d["open_data"] != true || d["export"] != true {
		t.Fatalf("en vigor = %v", d)
	}

	ahora = ahora.AddDate(0, 0, 35) // dentro de gracia (30+7)
	if d := get(); d["stamp_new"] != true {
		t.Fatalf("en gracia = %v", d)
	}

	ahora = ahora.AddDate(0, 0, 5) // gracia agotada
	d = get()
	if d["stamp_new"] != false || d["open_data"] != true || d["export"] != true {
		t.Fatalf("tras gracia = %v (abrir/exportar SIEMPRE true)", d)
	}
}

func TestLicencias404YValidacion(t *testing.T) {
	_, srv := nuevoAPIPrueba(t, time.Now)
	resp, _ := srv.Client().Get(srv.URL + "/v1/licenses/44444444-4444-4444-4444-444444444444")
	if resp.StatusCode != 404 {
		t.Fatalf("GET inexistente = %d", resp.StatusCode)
	}
	if r := put(t, srv, "no-uuid", `{"plan":"pro","dias_validez":0}`); r.StatusCode != 400 {
		t.Fatalf("id no UUID = %d, want 400", r.StatusCode)
	}
	if r := put(t, srv, "55555555-5555-5555-5555-555555555555", `{"plan":"pro","dias_validez":0}`); r.StatusCode != 422 {
		t.Fatalf("dias_validez 0 = %d, want 422", r.StatusCode)
	}
}
