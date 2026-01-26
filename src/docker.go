package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"time"
)

/// DockerClient provides minimal access to the Docker HTTP API.
type DockerClient struct {
	client *http.Client
}

/// NewDockerClient creates a Docker client bound to a Unix socket.
func NewDockerClient(sock string, timeout time.Duration) *DockerClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", sock, timeout)
		},
	}

	return &DockerClient{
		client: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
	}
}

/// DockerContainer represents a container summary.
type DockerContainer struct {
	ID     string            `json:"Id"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

/// DockerInspect represents a container inspection result.
type DockerInspect struct {
	ID     string `json:"Id"`
	Config struct {
		Labels      map[string]string `json:"Labels"`
		Healthcheck *struct {
			Interval int64 `json:"Interval"`
			Timeout  int64 `json:"Timeout"`
			Retries  int   `json:"Retries"`
		} `json:"Healthcheck"`
	} `json:"Config"`
	State struct {
		Health *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

/// ListContainers lists Docker containers.
func (d *DockerClient) ListContainers(ctx context.Context) ([]DockerContainer, error) {
	q := url.Values{}
	q.Set("all", "1")

	resp, err := d.do(ctx, "GET", "/containers/json", q)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out []DockerContainer
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

/// Inspect inspects a Docker container.
func (d *DockerClient) Inspect(ctx context.Context, id string) (*DockerInspect, error) {
	resp, err := d.do(ctx, "GET", "/containers/"+id+"/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out DockerInspect
	err = json.NewDecoder(resp.Body).Decode(&out)
	return &out, err
}

func (d *DockerClient) do(ctx context.Context, method, path string, q url.Values) (*http.Response, error) {
	u := "http://unix" + path
	if q != nil {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}

	return d.client.Do(req)
}

/// ContainerExists returns whether a container can be inspected successfully.
func (d *DockerClient) ContainerExists(ctx context.Context, id string) (bool, error) {
	resp, err := d.do(ctx, "GET", "/containers/"+id+"/json", nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return false, nil
	}
	if resp.StatusCode >= 400 {
		return false, http.ErrUseLastResponse
	}

	return true, nil
}
