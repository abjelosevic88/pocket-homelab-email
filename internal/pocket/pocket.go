// Package pocket talks to sibling Pocket apps: Pocket Habits (a day's
// practice ticks a habit) and Pocket Health (mindful minutes in, heart rate
// variability, resting heart rate and sleep out). Same bearer-token JSON API everywhere.
package pocket

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
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
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
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

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

// ---------------- Pocket Habits ----------------

// Habit is a Pocket Habits habit.
type Habit struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Days  string `json:"days"`
}

// Habits lists the person's habits.
func (c *Client) Habits(ctx context.Context) ([]Habit, error) {
	var out []Habit
	if err := c.do(ctx, http.MethodGet, "/api/v1/habits", nil, nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Habit{}
	}
	return out, nil
}

// MarkHabit ticks a habit on a date (status 1 = done).
func (c *Client) MarkHabit(ctx context.Context, habitID int64, date string) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/habits/%d/entries/%s", habitID, date), nil, map[string]any{"status": 1}, nil)
}

// ---------------- Pocket Health ----------------

// LogMindful records mindful minutes in Pocket Health: one sample per
// practice at its start time. The same start sent again updates it (a
// changed length, or 0 after the practice was deleted).
func (c *Client) LogMindful(ctx context.Context, at time.Time, minutes float64, note string) error {
	body := map[string]any{"metric": "mindful_min", "value": minutes, "unit": "min", "at": at.Format(time.RFC3339), "note": note, "source": "still"}
	return c.do(ctx, http.MethodPost, "/api/v1/samples", nil, body, nil)
}

// DayValue is one day of a Pocket Health metric.
type DayValue struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// Metric reads a metric's daily values for the days up to `to` (range: w, m, 3m, 6m, y).
func (c *Client) Metric(ctx context.Context, key, rng, to string) ([]DayValue, error) {
	var out struct {
		Points []DayValue `json:"points"`
	}
	q := url.Values{"range": {rng}}
	if to != "" {
		q.Set("to", to)
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/metrics/"+url.PathEscape(key), q, nil, &out); err != nil {
		return nil, err
	}
	return out.Points, nil
}
