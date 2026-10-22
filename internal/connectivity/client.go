// Package connectivity adapts NPLN ICE allocation to OpenPak's internal connectivity API.
package connectivity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("connectivity allocation unavailable")

type Client struct {
	base, token string
	http        *http.Client
}

type Identity struct {
	Title      string `json:"title"`
	Session    string `json:"session"`
	Player     string `json:"player"`
	Generation string `json:"generation"`
	Component  string `json:"component"`
}

type ICE struct {
	STUN      string    `json:"stun"`
	TURN      string    `json:"turn"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	ExpiresAt time.Time `json:"expires_at"`
}

// FromEnvironment returns nil only when the integration is explicitly unconfigured.
// Tokens live in a private mounted file, not a shared fleet-wide environment file.
func FromEnvironment() (*Client, error) {
	base, file := os.Getenv("NPLN_CONNECTIVITY_URL"), os.Getenv("NPLN_CONNECTIVITY_TOKEN_FILE")
	if base == "" && file == "" {
		return nil, nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return nil, ErrUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, ErrUnavailable
	}
	return New(base, strings.TrimSpace(string(b)))
}

func New(base, token string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(token) < 32 {
		return nil, ErrUnavailable
	}
	return &Client{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return ErrUnavailable
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return ErrUnavailable
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(r)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(out); err != nil {
		return ErrUnavailable
	}
	return nil
}

// Allocate registers the verified bearer generation, then obtains coturn credentials.
// NPLN requests ICE before a game room exists, so Session is an ICE bootstrap scope.
func (c *Client) Allocate(ctx context.Context, key Identity, country string, ttl int) (ICE, error) {
	var participant struct {
		ID string `json:"id"`
	}
	request := struct {
		Key        Identity `json:"key"`
		Candidates []any    `json:"candidates"`
		TTLSeconds int      `json:"ttl_seconds"`
	}{key, []any{}, ttl}
	if err := c.post(ctx, "/v1/participants", request, &participant); err != nil || participant.ID == "" {
		return ICE{}, ErrUnavailable
	}
	var ice ICE
	if err := c.post(ctx, "/v1/ice", map[string]string{"participant": participant.ID, "country": country}, &ice); err != nil {
		return ICE{}, err
	}
	if _, _, err := Endpoint(ice.STUN); err != nil {
		return ICE{}, err
	}
	if _, _, err := Endpoint(ice.TURN); err != nil {
		return ICE{}, err
	}
	if ice.Username == "" || ice.Password == "" || !ice.ExpiresAt.After(time.Now()) || ice.ExpiresAt.After(time.Now().Add(time.Duration(ttl+1)*time.Second)) {
		return ICE{}, ErrUnavailable
	}
	return ice, nil
}

func Endpoint(v string) (string, int32, error) {
	host, p, err := net.SplitHostPort(v)
	if err != nil || host == "" {
		return "", 0, ErrUnavailable
	}
	port, err := strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, ErrUnavailable
	}
	return host, int32(port), nil
}
