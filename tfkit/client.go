package tfkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is an authed JSON HTTP client a generated provider's resources use to
// reach the service's REST surface. It is the Terraform analog of the clikit
// authed transport: it attaches a bearer token (when set) and marshals/decodes
// JSON. It deliberately does not depend on a generated Go client — the enriched
// OpenAPI contract already tells the generator every path and field.
type Client struct {
	// BaseURL is the service base URL, e.g. "https://api.example.com".
	BaseURL string
	// Token is the bearer token attached to every request (empty = no auth).
	Token string
	// HTTP is the underlying client; defaults to http.DefaultClient.
	HTTP *http.Client
}

// NewClient returns a [Client] for baseURL with the given bearer token.
func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{}}
}

// Do issues an authed JSON request and decodes a 2xx body into out (when
// non-nil). body, when non-nil, is JSON-marshalled as the request payload. It
// is the single request primitive the CRUD helpers build on, mirroring the
// clikit doRequest generated helper.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if c == nil || c.BaseURL == "" {
		return fmt.Errorf("tfkit: no base URL configured on the provider")
	}
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s: %s", method, u, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// DoCreate POSTs body to a collection path and decodes the created resource
// into out.
func (c *Client) DoCreate(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, nil, body, out)
}

// DoRead GETs a resource path and decodes it into out.
func (c *Client) DoRead(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, nil, out)
}

// DoUpdate PATCHes a resource path (optionally with an updateMask query) and
// decodes the updated resource into out.
func (c *Client) DoUpdate(ctx context.Context, path string, query url.Values, body, out any) error {
	return c.Do(ctx, http.MethodPatch, path, query, body, out)
}

// DoDelete DELETEs a resource path.
func (c *Client) DoDelete(ctx context.Context, path string) error {
	return c.Do(ctx, http.MethodDelete, path, nil, nil, nil)
}
