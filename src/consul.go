package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

/// ConsulClient provides minimal access to the Consul agent HTTP API.
type ConsulClient struct {
	base   string
	token  string
	client *http.Client
	dryRun bool
}

/// NewConsulClient creates a Consul client.
func NewConsulClient(addr, token string, timeout time.Duration, dryRun bool) *ConsulClient {
	return &ConsulClient{
		base:   strings.TrimRight(addr, "/"),
		token: token,
		client: &http.Client{
			Timeout: timeout,
		},
		dryRun: dryRun,
	}
}

/// RegisterService registers a Consul service unless dry-run is enabled.
func (c *ConsulClient) RegisterService(ctx context.Context, def map[string]any) error {
	if c.dryRun {
		return nil
	}

	q := url.Values{}
	q.Set("replace-existing-checks", "true")

	return c.do(ctx, "PUT", "/v1/agent/service/register", q, def)
}

/// DeregisterService deregisters a Consul service unless dry-run is enabled.
func (c *ConsulClient) DeregisterService(ctx context.Context, id, ns, partition string) error {
	if c.dryRun {
		return nil
	}

	q := url.Values{}
	if ns != "" {
		q.Set("ns", ns)
	}
	if partition != "" {
		q.Set("partition", partition)
	}

	return c.do(ctx, "PUT", "/v1/agent/service/deregister/"+url.PathEscape(id), q, nil)
}

/// PassCheck marks a TTL check as passing unless dry-run is enabled.
func (c *ConsulClient) PassCheck(ctx context.Context, checkID, ns, note string) error {
	if c.dryRun {
		return nil
	}

	q := url.Values{}
	if ns != "" {
		q.Set("ns", ns)
	}
	if note != "" {
		q.Set("note", note)
	}

	return c.do(ctx, "PUT", "/v1/agent/check/pass/"+url.PathEscape(checkID), q, nil)
}

func (c *ConsulClient) do(ctx context.Context, method, path string, q url.Values, body any) error {
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}

	u := c.base + path
	if q != nil {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("X-Consul-Token", c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return http.ErrUseLastResponse
	}

	return nil
}
