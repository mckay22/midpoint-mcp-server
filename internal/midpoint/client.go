package midpoint

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// restPrefix is joined to Config.BaseURL to form the REST base.
const restPrefix = "/ws/rest"

// maxResponseBytes caps how much of a midPoint response we read, so a
// misbehaving or unexpected endpoint can't exhaust memory.
const maxResponseBytes = 4 << 20 // 4 MiB

// Client talks to a midPoint deployment's REST API using HTTP Basic auth.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient returns a Client for the given config. It configures a sane
// request timeout and, only when Config.InsecureTLS is set, a transport that
// skips certificate verification (for self-signed dev instances).
func NewClient(cfg Config) *Client {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if cfg.InsecureTLS {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in dev-only escape hatch
		}
	}
	return &Client{cfg: cfg, http: httpClient}
}

// Identity is the authenticated principal reported by /ws/rest/self.
type Identity struct {
	OID          string
	Name         string
	FullName     string
	EmailAddress string
}

// Self returns the identity midPoint associates with the configured
// credentials by calling GET /ws/rest/self. It is the connectivity/identity
// check behind the ping tool.
func (c *Client) Self(ctx context.Context) (Identity, error) {
	body, err := c.get(ctx, "/self")
	if err != nil {
		return Identity{}, err
	}

	var resp selfResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return Identity{}, fmt.Errorf("decoding /self response: %w", err)
	}
	return Identity{
		OID:          resp.User.OID,
		Name:         resp.User.Name.value(),
		FullName:     resp.User.FullName.value(),
		EmailAddress: resp.User.EmailAddress,
	}, nil
}

// get performs an authenticated GET against restPrefix+path and returns the
// response body for any 2xx status. Errors carry the path and status but never
// the credentials.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	url := strings.TrimRight(c.cfg.BaseURL, "/") + restPrefix + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", path, err)
	}
	req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling midPoint %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading %s response: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("midPoint %s: unexpected status %s", path, resp.Status)
	}
	return body, nil
}

// selfResponse mirrors midPoint's JSON envelope for a focus object, which wraps
// the object under a key named for its type ("user" for the REST principal).
type selfResponse struct {
	User struct {
		OID          string     `json:"oid"`
		Name         polyString `json:"name"`
		FullName     polyString `json:"fullName"`
		EmailAddress string     `json:"emailAddress"`
	} `json:"user"`
}

// polyString decodes a midPoint PolyStringType, which serializes either as a
// bare JSON string or as an object like {"orig":"...","norm":"..."}.
type polyString struct {
	Orig string
}

func (p polyString) value() string { return p.Orig }

func (p *polyString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		p.Orig = s
		return nil
	}
	var obj struct {
		Orig string `json:"orig"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	p.Orig = obj.Orig
	return nil
}
