package sello

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.2.3", "1.2.3", 0},
		{"v prefix equal", "v1.2.3", "1.2.3", 0},
		{"major wins", "2.0.0", "1.9.9", 1},
		{"older major", "1.9.9", "2.0.0", -1},
		{"minor wins", "1.10.0", "1.2.0", 1},
		{"older minor", "1.2.0", "1.10.0", -1},
		{"patch wins", "1.2.10", "1.2.3", 1},
		{"older patch", "1.2.3", "1.2.10", -1},
		{"prerelease below release", "1.2.3-rc1", "1.2.3", -1},
		{"release above prerelease", "1.2.3", "1.2.3-rc1", 1},
		{"numeric prerelease order", "1.2.3-rc.2", "1.2.3-rc.10", -1},
		{"alphanumeric prerelease order", "1.2.3-alpha", "1.2.3-beta", -1},
		{"current dev suffix vs next release", "0.1.0-unstable", "0.2.0", -1},
		{"same prerelease", "1.0.0-rc.1", "1.0.0-rc.1", 0},
		{"longer prerelease wins tie", "1.0.0-rc", "1.0.0-rc.1", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compareSemver(tt.a, tt.b)
			if err != nil {
				t.Fatalf("compareSemver(%q, %q): %v", tt.a, tt.b, err)
			}
			if got != tt.want {
				t.Fatalf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCompareSemverRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "abc", "1", "1.2", "1.2.x", "1.2.3.4", "x1.2.3"} {
		if _, err := compareSemver(bad, "1.0.0"); err == nil {
			t.Fatalf("compareSemver(%q) accepted a malformed version", bad)
		}
	}
}

// fakeAPI stands up a stub of GET /v1/version/latest answering the given
// status and body, and returns an Updater pointed at it.
func fakeAPI(t *testing.T, status int, body string) *Updater {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewUpdater(srv.URL)
}

func TestCheckForUpdate(t *testing.T) {
	newer := `{"data":{"version":"1.2.0","url":"https://dl.facturable.es/sello-1.2.0","sha256":"abc123"}}`

	t.Run("newer version returns info", func(t *testing.T) {
		u := fakeAPI(t, http.StatusOK, newer)
		info, err := u.CheckForUpdate("1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if info == nil {
			t.Fatal("CheckForUpdate = nil, want update info")
		}
		if info.Version != "1.2.0" || info.URL != "https://dl.facturable.es/sello-1.2.0" || info.SHA256 != "abc123" {
			t.Fatalf("info = %+v", info)
		}
	})

	t.Run("up to date returns nil", func(t *testing.T) {
		u := fakeAPI(t, http.StatusOK, newer)
		info, err := u.CheckForUpdate("1.2.0")
		if err != nil || info != nil {
			t.Fatalf("CheckForUpdate = %+v, %v; want nil, nil", info, err)
		}
	})

	t.Run("older local version than latest still updates", func(t *testing.T) {
		u := fakeAPI(t, http.StatusOK, newer)
		info, err := u.CheckForUpdate("2.0.0")
		if err != nil || info != nil {
			t.Fatalf("latest older than current: %+v, %v; want nil, nil", info, err)
		}
	})

	t.Run("server error surfaces", func(t *testing.T) {
		u := fakeAPI(t, http.StatusInternalServerError, "boom")
		if _, err := u.CheckForUpdate("1.0.0"); err == nil {
			t.Fatal("HTTP 500 accepted")
		}
	})

	t.Run("malformed body surfaces", func(t *testing.T) {
		u := fakeAPI(t, http.StatusOK, "{not json")
		if _, err := u.CheckForUpdate("1.0.0"); err == nil {
			t.Fatal("malformed JSON accepted")
		}
	})
}

func TestDownloadAndVerify(t *testing.T) {
	payload := []byte("facturable-binary")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	download := func(t *testing.T, content []byte) string {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(content)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}

	t.Run("matching digest returns bytes", func(t *testing.T) {
		u := NewUpdater("http://unused.invalid")
		raw, err := u.DownloadAndVerify(&UpdateInfo{URL: download(t, payload), SHA256: digest})
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != string(payload) {
			t.Fatalf("raw = %q, want %q", raw, payload)
		}
	})

	t.Run("uppercase digest accepted", func(t *testing.T) {
		u := NewUpdater("http://unused.invalid")
		if _, err := u.DownloadAndVerify(&UpdateInfo{URL: download(t, payload), SHA256: "SHA256:" + hex.EncodeToString(sum[:])}); err != nil {
			t.Fatalf("uppercase digest rejected: %v", err)
		}
	})

	t.Run("mismatched digest refused", func(t *testing.T) {
		u := NewUpdater("http://unused.invalid")
		if _, err := u.DownloadAndVerify(&UpdateInfo{URL: download(t, payload), SHA256: "deadbeef"}); err == nil {
			t.Fatal("tampered download accepted")
		}
	})

	t.Run("missing digest refused", func(t *testing.T) {
		u := NewUpdater("http://unused.invalid")
		if _, err := u.DownloadAndVerify(&UpdateInfo{URL: download(t, payload)}); err == nil {
			t.Fatal("download without published digest accepted")
		}
	})
}
