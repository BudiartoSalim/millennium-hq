// Package registry asks package registries when a version was published
// and OSV which versions have known vulnerabilities. Every request has a
// timeout and a response size limit; any failure is an error, never a pass.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"millennium/internal/domain"
)

const (
	requestTimeout   = 20 * time.Second
	maxResponseBytes = 32 << 20
	osvBatchSize     = 1000
)

// Endpoints are the registries' base URLs; tests point them at a local
// server.
type Endpoints struct {
	NPM, GoProxy, PyPI, OSV string
}

// Public are the public registries.
func Public() Endpoints {
	return Endpoints{
		NPM: "https://registry.npmjs.org", GoProxy: "https://proxy.golang.org",
		PyPI: "https://pypi.org", OSV: "https://api.osv.dev",
	}
}

// Client talks to the registries.
type Client struct {
	endpoints Endpoints
	http      *http.Client
}

// New returns a client for the endpoints.
func New(endpoints Endpoints) *Client {
	return &Client{endpoints: endpoints, http: &http.Client{Timeout: requestTimeout}}
}

// PublishedAt is when a version was released.
func (c *Client) PublishedAt(ctx context.Context, p domain.Package) (time.Time, error) {
	switch p.Ecosystem {
	case domain.EcosystemNPM:
		return c.npmTime(ctx, p)
	case domain.EcosystemGo:
		return c.goTime(ctx, p)
	case domain.EcosystemPyPI:
		return c.pypiTime(ctx, p)
	}
	return time.Time{}, fmt.Errorf("unknown ecosystem %q", p.Ecosystem)
}

func (c *Client) npmTime(ctx context.Context, p domain.Package) (time.Time, error) {
	var doc struct {
		Time map[string]time.Time `json:"time"`
	}
	if err := c.getJSON(ctx, c.endpoints.NPM+"/"+url.PathEscape(p.Name), &doc); err != nil {
		return time.Time{}, err
	}
	published, ok := doc.Time[p.Version]
	if !ok {
		return time.Time{}, fmt.Errorf("npm has no release %s", p.Key())
	}
	return published, nil
}

func (c *Client) goTime(ctx context.Context, p domain.Package) (time.Time, error) {
	var info struct {
		Time time.Time `json:"Time"`
	}
	target := fmt.Sprintf("%s/%s/@v/%s.info", c.endpoints.GoProxy, escapeModule(p.Name), escapeModule(p.Version))
	if err := c.getJSON(ctx, target, &info); err != nil {
		return time.Time{}, err
	}
	return info.Time, nil
}

func (c *Client) pypiTime(ctx context.Context, p domain.Package) (time.Time, error) {
	var doc struct {
		URLs []struct {
			Uploaded time.Time `json:"upload_time_iso_8601"`
		} `json:"urls"`
	}
	target := fmt.Sprintf("%s/pypi/%s/%s/json", c.endpoints.PyPI, url.PathEscape(p.Name), url.PathEscape(p.Version))
	if err := c.getJSON(ctx, target, &doc); err != nil {
		return time.Time{}, err
	}
	if len(doc.URLs) == 0 {
		return time.Time{}, fmt.Errorf("PyPI lists no files for %s", p.Key())
	}
	earliest := doc.URLs[0].Uploaded
	for _, u := range doc.URLs[1:] {
		if u.Uploaded.Before(earliest) {
			earliest = u.Uploaded
		}
	}
	return earliest, nil
}

// escapeModule applies the module proxy's case encoding: an upper-case
// letter becomes "!" and its lower case.
func escapeModule(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsUpper(r) {
			b.WriteRune('!')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

type osvQuery struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version"`
}

// Vulnerabilities asks OSV about every package, in batches.
func (c *Client) Vulnerabilities(ctx context.Context, packages []domain.Package) (map[string][]string, error) {
	found := map[string][]string{}
	for start := 0; start < len(packages); start += osvBatchSize {
		batch := packages[start:min(start+osvBatchSize, len(packages))]
		if err := c.osvBatch(ctx, batch, found); err != nil {
			return nil, err
		}
	}
	return found, nil
}

func (c *Client) osvBatch(ctx context.Context, batch []domain.Package, found map[string][]string) error {
	queries := make([]osvQuery, len(batch))
	for i, p := range batch {
		queries[i].Package.Name, queries[i].Package.Ecosystem, queries[i].Version = p.Name, osvEcosystem(p.Ecosystem), p.Version
	}
	body, err := json.Marshal(map[string]any{"queries": queries})
	if err != nil {
		return err
	}
	var result struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if err := c.postJSON(ctx, c.endpoints.OSV+"/v1/querybatch", body, &result); err != nil {
		return err
	}
	if len(result.Results) != len(batch) {
		return fmt.Errorf("OSV answered %d results for %d queries", len(result.Results), len(batch))
	}
	for i, r := range result.Results {
		for _, v := range r.Vulns {
			found[batch[i].String()] = append(found[batch[i].String()], v.ID)
		}
	}
	return nil
}

func osvEcosystem(e domain.Ecosystem) string {
	switch e {
	case domain.EcosystemGo:
		return "Go"
	case domain.EcosystemPyPI:
		return "PyPI"
	}
	return "npm"
}

func (c *Client) getJSON(ctx context.Context, target string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	return c.do(req, into)
}

func (c *Client) postJSON(ctx context.Context, target string, body []byte, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, into)
}

func (c *Client) do(req *http.Request, into any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: status %d", req.Method, req.URL.Redacted(), resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", req.URL.Host, err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("the registry's answer is too large")
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("read %s: %w", req.URL.Host, err)
	}
	return nil
}
