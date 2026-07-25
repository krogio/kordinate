package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// http.go is the shared transport for every live client: one place that owns
// timeouts, error shaping and logging, so the per-service clients are just
// path + payload.

// Config is the set of upstream endpoints and credentials, read from env.
type Config struct {
	// Mode is "live" or "fake". Anything other than "live" uses the fakes, so a
	// missing/typo'd value fails safe to fake data rather than firing requests
	// at a half-configured endpoint.
	Mode string

	CustomerServiceURL string
	ClaireAPIURL       string
	ClaireFileURL      string
	UMLURL             string
	UOPSAPIURL         string
	EmmaAPIURL         string
	IDVServiceURL      string
	DeviceBlockerURL   string
	VMSAPIURL          string

	// Claire uses OAuth1-style consumer credentials (carried over from
	// claire-admin's CLAIRE_OAUTH_* env vars).
	ClaireConsumerKey    string
	ClaireConsumerSecret string
	ClaireToken          string
	ClaireTokenSecret    string

	// Timeout bounds every upstream call. A back-office UI must fail fast: an
	// agent on the phone to a customer needs an error, not a spinner.
	Timeout time.Duration
}

// Live reports whether live clients should be used.
func (c Config) Live() bool { return strings.EqualFold(strings.TrimSpace(c.Mode), "live") }

// FromEnv builds Config from environment variables, keeping claire-admin's
// names so existing deployment config carries over unchanged.
func FromEnv(get func(string) string) Config {
	c := Config{
		Mode:                 get("UPSTREAM_MODE"),
		CustomerServiceURL:   get("CUSTOMER_SERVICE_URL"),
		ClaireAPIURL:         get("CLAIRE_API_URL"),
		ClaireFileURL:        get("CLAIRE_API_FILE_URL"),
		UMLURL:               get("UML_URL"),
		UOPSAPIURL:           get("UOPS_API_URL"),
		EmmaAPIURL:           get("EMMA_API"),
		IDVServiceURL:        get("IDV_SERVICE_API_URL"),
		DeviceBlockerURL:     get("DEVICE_BLOCKER_SERVICE_URL"),
		VMSAPIURL:            get("VMS_API_URL"),
		ClaireConsumerKey:    get("CLAIRE_OAUTH_CONSUMER_KEY"),
		ClaireConsumerSecret: get("CLAIRE_OAUTH_CONSUMER_SECRET"),
		ClaireToken:          get("CLAIRE_OAUTH_TOKEN"),
		ClaireTokenSecret:    get("CLAIRE_OAUTH_TOKEN_SECRET"),
	}
	if c.Mode == "" {
		c.Mode = "fake"
	}
	c.Timeout = 20 * time.Second
	return c
}

// Error is an upstream failure carrying the service and status, so handlers can
// distinguish "customer not found" (404) from "the service is down" (5xx) and
// tell the agent something useful.
type Error struct {
	Service string
	Status  int
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s (status %d)", e.Service, e.Message, e.Status)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Service, e.Err)
	}
	return fmt.Sprintf("%s: status %d", e.Service, e.Status)
}

func (e *Error) Unwrap() error { return e.Err }

// NotFound reports whether the upstream said the resource doesn't exist.
func (e *Error) NotFound() bool { return e.Status == http.StatusNotFound }

// Unavailable reports whether this was a transport or server-side failure —
// the class of error worth retrying or surfacing as "service unavailable"
// rather than as a data problem.
func (e *Error) Unavailable() bool { return e.Status == 0 || e.Status >= 500 }

// client is the shared HTTP caller for one upstream service.
type client struct {
	name    string
	baseURL string
	hc      *http.Client
	// headers are added to every request (auth, content negotiation).
	headers map[string]string
}

func newClient(name, baseURL string, timeout time.Duration) *client {
	return &client{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		hc:      &http.Client{Timeout: timeout},
		headers: map[string]string{},
	}
}

// do performs a request and decodes a JSON response into out (which may be nil
// for calls that return no body).
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	if c.baseURL == "" {
		return &Error{Service: c.name, Message: "no base URL configured"}
	}

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return &Error{Service: c.name, Err: fmt.Errorf("encode request: %w", err)}
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return &Error{Service: c.name, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "upstream call failed", "service", c.name, "method", method, "path", path, "err", err)
		return &Error{Service: c.name, Err: err}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	slog.DebugContext(ctx, "upstream call", "service", c.name, "method", method,
		"path", path, "status", resp.StatusCode, "ms", time.Since(start).Milliseconds())

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Error{Service: c.name, Status: resp.StatusCode, Message: errMessage(raw)}
	}
	if readErr != nil {
		return &Error{Service: c.name, Status: resp.StatusCode, Err: fmt.Errorf("read body: %w", readErr)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &Error{Service: c.name, Status: resp.StatusCode, Err: fmt.Errorf("decode response: %w", err)}
	}
	return nil
}

// doMultipart performs a request with a pre-built multipart body. Document
// upload is the one write that isn't JSON, so the caller assembles the body and
// this keeps the shared error/logging path.
func (c *client) doMultipart(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	if c.baseURL == "" {
		return &Error{Service: c.name, Message: "no base URL configured"}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return &Error{Service: c.name, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "upstream upload failed", "service", c.name, "method", method, "path", path, "err", err)
		return &Error{Service: c.name, Err: err}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	slog.DebugContext(ctx, "upstream upload", "service", c.name, "method", method,
		"path", path, "status", resp.StatusCode, "ms", time.Since(start).Milliseconds())

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Error{Service: c.name, Status: resp.StatusCode, Message: errMessage(raw)}
	}
	if readErr != nil {
		return &Error{Service: c.name, Status: resp.StatusCode, Err: fmt.Errorf("read body: %w", readErr)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &Error{Service: c.name, Status: resp.StatusCode, Err: fmt.Errorf("decode response: %w", err)}
	}
	return nil
}

// doRaw performs a request and returns the raw body plus content type — used
// for document downloads, where the payload is a file rather than JSON.
func (c *client) doRaw(ctx context.Context, method, path string) ([]byte, string, error) {
	if c.baseURL == "" {
		return nil, "", &Error{Service: c.name, Message: "no base URL configured"}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, "", &Error{Service: c.name, Err: err}
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", &Error{Service: c.name, Err: err}
	}
	defer resp.Body.Close()

	// 32 MiB ceiling: FICA scans are photos, not videos — anything larger is a
	// misconfiguration and must not be pulled into memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", &Error{Service: c.name, Status: resp.StatusCode, Message: errMessage(raw)}
	}
	if err != nil {
		return nil, "", &Error{Service: c.name, Err: fmt.Errorf("read body: %w", err)}
	}
	return raw, resp.Header.Get("Content-Type"), nil
}

// errMessage pulls a human-readable message out of an upstream error body.
// These services are inconsistent — some send {"error": "..."}, some
// {"error": {"message": "..."}}, some {"message": "..."} — so try each.
func errMessage(raw []byte) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var shaped struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Errors  json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(raw, &shaped); err == nil {
		if shaped.Message != "" {
			return shaped.Message
		}
		if len(shaped.Error) > 0 {
			var s string
			if json.Unmarshal(shaped.Error, &s) == nil && s != "" {
				return s
			}
			var nested struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(shaped.Error, &nested) == nil && nested.Message != "" {
				return nested.Message
			}
			return string(shaped.Error)
		}
		if len(shaped.Errors) > 0 {
			return string(shaped.Errors)
		}
	}
	// Unstructured body — return a bounded snippet so the agent sees something
	// actionable without a wall of HTML in the UI.
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// q builds a query string from non-empty pairs, so callers can pass optional
// filters without conditional string building at each site.
func q(pairs map[string]string) string {
	v := url.Values{}
	for k, val := range pairs {
		if val != "" {
			v.Set(k, val)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}
