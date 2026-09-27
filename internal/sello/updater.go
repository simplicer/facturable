package sello

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxUpdateBytes caps an agent binary download (the swap never reads more).
const maxUpdateBytes = 256 << 20

// UpdateInfo describes one newer agent build published by the API.
type UpdateInfo struct {
	Version string `json:"version"` // latest semver (e.g. "1.2.3")
	URL     string `json:"url"`     // where the new binary lives
	SHA256  string `json:"sha256"`  // hex digest the download must match
}

// Updater keeps the local agent current: the founder's promise is that the
// agent follows every Veri*Factu rule change without the user reinstalling
// anything. Checks hit the public API; the binary swap only proceeds once
// the download matches the published SHA-256.
type Updater struct {
	APIBase string // e.g. https://api.facturable.es (FACTURABLE_API_URL)
	// HTTP is injectable for tests; zero value means a 2-minute timeout.
	HTTP *http.Client
}

// NewUpdater builds the auto-update client against the public API base.
func NewUpdater(apiBase string) *Updater {
	return &Updater{APIBase: apiBase, HTTP: &http.Client{Timeout: 2 * time.Minute}}
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// CheckForUpdate asks the API for the latest agent build and returns it only
// when strictly newer than currentVersion; nil (with nil error) means the
// installation is up to date.
func (u *Updater) CheckForUpdate(currentVersion string) (*UpdateInfo, error) {
	resp, err := u.client().Get(strings.TrimRight(u.APIBase, "/") + "/v1/version/latest")
	if err != nil {
		return nil, fmt.Errorf("no se pudo consultar la última versión: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("consulta de la última versión: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data UpdateInfo `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("respuesta de versión ilegible: %w", err)
	}
	latest := payload.Data
	if latest.Version == "" {
		return nil, fmt.Errorf("respuesta de versión sin campo 'version'")
	}
	newer, err := compareSemver(latest.Version, currentVersion)
	if err != nil {
		return nil, err
	}
	if newer <= 0 {
		return nil, nil // up to date
	}
	return &latest, nil
}

// DownloadAndVerify fetches the new binary and enforces its SHA-256 digest:
// a corrupted or tampered download never becomes the running binary.
func (u *Updater) DownloadAndVerify(info *UpdateInfo) ([]byte, error) {
	if info == nil {
		return nil, fmt.Errorf("no hay ninguna actualización que descargar")
	}
	if info.URL == "" {
		return nil, fmt.Errorf("la actualización no indica URL de descarga")
	}
	want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(info.SHA256)), "sha256:")
	if want == "" {
		return nil, fmt.Errorf("la actualización no indica suma SHA-256; descarga cancelada")
	}
	resp, err := u.client().Get(info.URL)
	if err != nil {
		return nil, fmt.Errorf("no se pudo descargar la actualización: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("descarga de la actualización: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateBytes))
	if err != nil {
		return nil, fmt.Errorf("descarga incompleta: %w", err)
	}
	got := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return nil, fmt.Errorf("el binario descargado no coincide con la suma SHA-256 publicada; actualización cancelada")
	}
	return raw, nil
}

// ApplyUpdate downloads, verifies and swaps the binary: the new build lands
// in /tmp, the running executable moves to <exe>.bak and the new one takes
// its place. A failed swap restores the backup so the agent stays runnable;
// the new version applies on next restart.
func (u *Updater) ApplyUpdate(info *UpdateInfo) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no se localizó el binario en ejecución: %w", err)
	}
	raw, err := u.DownloadAndVerify(info)
	if err != nil {
		return err
	}
	staged := filepath.Join(os.TempDir(), "facturable-update-"+info.Version)
	if err := os.WriteFile(staged, raw, 0o755); err != nil {
		return fmt.Errorf("no se pudo escribir %s: %w", staged, err)
	}
	backup := exe + ".bak"
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("no se pudo respaldar el binario actual: %w", err)
	}
	if err := os.Rename(staged, exe); err != nil {
		if restore := os.Rename(backup, exe); restore != nil {
			return fmt.Errorf("el reemplazo falló (%v) y la restauración del respaldo también (%v)", err, restore)
		}
		return err
	}
	return nil
}

// compareSemver compares two semantic versions: -1 when a < b, 0 when equal,
// 1 when a > b. Pre-release suffixes rank below the same release
// ("1.2.3-rc1" < "1.2.3"); numeric pre-release identifiers compare
// numerically ("rc.2" < "rc.10").
func compareSemver(a, b string) (int, error) {
	acore, apre, err := splitSemver(a)
	if err != nil {
		return 0, err
	}
	bcore, bpre, err := splitSemver(b)
	if err != nil {
		return 0, err
	}
	for i := range acore {
		if acore[i] == bcore[i] {
			continue
		}
		if acore[i] < bcore[i] {
			return -1, nil
		}
		return 1, nil
	}
	return comparePrerelease(apre, bpre), nil
}

// splitSemver breaks "vX.Y.Z-pre" into its numeric core and pre-release.
func splitSemver(v string) ([3]int, string, error) {
	var out [3]int
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, "", fmt.Errorf("la versión %q no es semver (se esperaba X.Y.Z)", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, "", fmt.Errorf("la versión %q no es semver (se esperaba X.Y.Z)", v)
		}
		out[i] = n
	}
	return out, pre, nil
}

// comparePrerelease orders pre-release suffixes per semver: a release beats
// any pre-release, and fewer identifiers rank below more when all preceding
// ones are equal ("rc" < "rc.1").
func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := comparePrereleaseID(as[i], bs[i]); c != 0 {
			return c
		}
	}
	if len(as) == len(bs) {
		return 0
	}
	if len(as) < len(bs) {
		return -1
	}
	return 1
}

// comparePrereleaseID orders one dot-separated identifier: numerics compare
// numerically and rank below alphanumerics.
func comparePrereleaseID(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}
