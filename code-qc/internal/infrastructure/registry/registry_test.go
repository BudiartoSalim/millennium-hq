package registry

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"millennium-code-qc/internal/domain"
)

func fakeRegistries(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/npm/@scope%2Fpkg", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"time":{"1.0.0":"2026-01-02T03:04:05Z"}}`)
	})
	mux.HandleFunc("/go/github.com/!burnt!sushi/toml/@v/v1.4.0.info", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"Version":"v1.4.0","Time":"2025-06-01T00:00:00Z"}`)
	})
	mux.HandleFunc("/pypi/pypi/numpy/2.1.0/json", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"urls":[{"upload_time_iso_8601":"2026-03-02T00:00:00Z"},{"upload_time_iso_8601":"2026-03-01T00:00:00Z"}]}`)
	})
	mux.HandleFunc("/osv/v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries []osvQuery `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		results := make([]map[string]any, len(body.Queries))
		for i, q := range body.Queries {
			results[i] = map[string]any{}
			if q.Package.Name == "lodash" && q.Package.Ecosystem == "npm" {
				results[i]["vulns"] = []map[string]string{{"id": "GHSA-35jh-r3h4-6jhm"}}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return New(Endpoints{NPM: server.URL + "/npm", GoProxy: server.URL + "/go", PyPI: server.URL + "/pypi", OSV: server.URL + "/osv"})
}

func TestPublishedAt(t *testing.T) {
	c := fakeRegistries(t)
	tests := []struct {
		p    domain.Package
		want string
	}{
		{domain.Package{Ecosystem: domain.EcosystemNPM, Name: "@scope/pkg", Version: "1.0.0"}, "2026-01-02T03:04:05Z"},
		{domain.Package{Ecosystem: domain.EcosystemGo, Name: "github.com/BurntSushi/toml", Version: "v1.4.0"}, "2025-06-01T00:00:00Z"},
		{domain.Package{Ecosystem: domain.EcosystemPyPI, Name: "numpy", Version: "2.1.0"}, "2026-03-01T00:00:00Z"},
	}
	for _, tt := range tests {
		got, err := c.PublishedAt(t.Context(), tt.p)
		if err != nil || got.Format(time.RFC3339) != tt.want {
			t.Errorf("PublishedAt(%s) = %v, %v; want %s", tt.p, got, err, tt.want)
		}
	}
}

func TestPublishedAtFailures(t *testing.T) {
	c := fakeRegistries(t)
	for _, p := range []domain.Package{
		{Ecosystem: domain.EcosystemNPM, Name: "@scope/pkg", Version: "9.9.9"},
		{Ecosystem: domain.EcosystemGo, Name: "example.com/missing", Version: "v1.0.0"},
		{Ecosystem: "cargo", Name: "x", Version: "1"},
	} {
		if _, err := c.PublishedAt(t.Context(), p); err == nil {
			t.Errorf("PublishedAt(%s) must fail", p)
		}
	}
	unreachable := New(Endpoints{NPM: "http://127.0.0.1:1"})
	if _, err := unreachable.PublishedAt(t.Context(), domain.Package{Ecosystem: domain.EcosystemNPM, Name: "x", Version: "1"}); err == nil {
		t.Fatal("an unreachable registry must be an error")
	}
}

func TestVulnerabilities(t *testing.T) {
	c := fakeRegistries(t)
	found, err := c.Vulnerabilities(t.Context(), []domain.Package{
		{Ecosystem: domain.EcosystemNPM, Name: "lodash", Version: "4.17.20"},
		{Ecosystem: domain.EcosystemGo, Name: "lodash", Version: "1"},
	})
	if err != nil || len(found) != 1 || !slices.Equal(found["npm lodash@4.17.20"], []string{"GHSA-35jh-r3h4-6jhm"}) {
		t.Fatalf("found = %v, %v", found, err)
	}
	if escapeModule("github.com/BurntSushi/toml") != "github.com/!burnt!sushi/toml" {
		t.Fatal("module path escaping")
	}
	if osvEcosystem(domain.EcosystemPyPI) != "PyPI" || osvEcosystem(domain.EcosystemGo) != "Go" {
		t.Fatal("OSV ecosystem names")
	}
}

func TestOSVMismatchedAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(server.Close)
	_, err := New(Endpoints{OSV: server.URL}).Vulnerabilities(t.Context(), []domain.Package{{Ecosystem: domain.EcosystemNPM, Name: "x", Version: "1"}})
	if err == nil || !strings.Contains(err.Error(), "0 results for 1 queries") {
		t.Fatalf("err = %v", err)
	}
}
