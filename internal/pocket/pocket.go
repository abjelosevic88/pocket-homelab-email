// Package pocket talks to sibling Pocket apps (Documents, Files, Tasks,
// Calendar, Contacts, Notes, Money) and to Immich. Same bearer-token JSON
// API everywhere; uploads are multipart or a raw body.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	// APIKeyHeader sends the token in this header instead of a bearer
	// Authorization (Immich: "x-api-key").
	APIKeyHeader string
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any) error {
	var rd io.Reader
	ct := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, ct = bytes.NewReader(b), "application/json"
	}
	return c.send(ctx, method, path, q, rd, ct, v)
}

// send makes one request with a ready body and decodes a JSON answer into v.
func (c *Client) send(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string, v any) error {
	res, err := c.raw(ctx, method, path, q, body, contentType)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// raw makes one request and returns a successful response (the caller closes it).
func (c *Client) raw(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (*http.Response, error) {
	if !c.Configured() {
		return nil, errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if c.APIKeyHeader != "" {
		req.Header.Set(c.APIKeyHeader, c.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 300 {
		return res, nil
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("the token was rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	var e struct {
		Error   string `json:"error"`
		Message any    `json:"message"`
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
	_ = json.Unmarshal(b, &e)
	if e.Error != "" {
		return nil, &HTTPError{Status: res.StatusCode, Msg: e.Error}
	}
	if m, ok := e.Message.(string); ok && m != "" {
		return nil, &HTTPError{Status: res.StatusCode, Msg: m}
	}
	return nil, &HTTPError{Status: res.StatusCode}
}

// HTTPError is a non-2xx answer with the app's own message.
type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// JSON calls a JSON endpoint (body nil = none) and decodes the answer into v (nil = ignore).
func (c *Client) JSON(ctx context.Context, method, path string, q url.Values, body, v any) error {
	return c.do(ctx, method, path, q, body, v)
}

// Upload posts one file as multipart/form-data under field, with extra text fields.
func (c *Client) Upload(ctx context.Context, method, path string, q url.Values, field, filename string, data []byte, fields map[string]string, v any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, val := range fields {
		if err := mw.WriteField(k, val); err != nil {
			return err
		}
	}
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	return c.send(ctx, method, path, q, &buf, mw.FormDataContentType(), v)
}

// PutRaw sends bytes as the request body (Pocket Files' upload).
func (c *Client) PutRaw(ctx context.Context, path string, q url.Values, data []byte, contentType string, v any) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return c.send(ctx, http.MethodPut, path, q, bytes.NewReader(data), contentType, v)
}

// Download reads a file endpoint, at most max bytes (ErrTooBig past it).
func (c *Client) Download(ctx context.Context, path string, q url.Values, max int64) ([]byte, string, error) {
	res, err := c.raw(ctx, http.MethodGet, path, q, nil, "")
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > max {
		return nil, "", ErrTooBig
	}
	return b, res.Header.Get("Content-Type"), nil
}

// ErrTooBig is a download past the size limit.
var ErrTooBig = errors.New("the file is too big")

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}
