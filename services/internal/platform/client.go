package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// No retries, no circuit breaker.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: timeout}}
}

type UpstreamError struct {
	Status int
	URL    string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream %s returned %d", e.URL, e.Status)
}

func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &UpstreamError{Status: resp.StatusCode, URL: req.URL.String()}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
