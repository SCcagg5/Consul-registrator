package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	ID   string `json:"Id"`
	Name string `json:"Name"`
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
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
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
		return false, fmt.Errorf("docker inspect failed: %s", resp.Status)
	}
	return true, nil
}

/// StartContainer starts a container by ID or name.
/// Docker returns 204 (started) or 304 (already started).
func (d *DockerClient) StartContainer(ctx context.Context, idOrName string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/containers/"+idOrName+"/start", nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || resp.StatusCode == 304 {
		return nil
	}
	return fmt.Errorf("start failed for %s: %s", idOrName, resp.Status)
}

func normalizeAddr(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return in
	}
	// Accept http(s)://host:port or host:port
	if strings.Contains(in, "://") {
		if u, err := url.Parse(in); err == nil && u.Host != "" {
			return u.Host
		}
	}
	in = strings.TrimPrefix(in, "http://")
	in = strings.TrimPrefix(in, "https://")
	return in
}

/// LaunchSidecar creates and starts a new container as a sidecar proxy.
func (d *DockerClient) LaunchSidecar(ctx context.Context, parentID, name, serviceID string, cfg *Config) error {
	containerName := "consul-sidecar-" + strings.ReplaceAll(serviceID, ":", "_")

	grpcAddr := normalizeAddr(cfg.SidecarGrpcAddr)
	httpAddr := strings.TrimSpace(cfg.SidecarHttpAddr)

	// IMPORTANT: override image ENTRYPOINT, otherwise consul_proxy's /bin/entrypoint.sh runs
	// and ignores our -sidecar-for <serviceID>.
	entrypoint := []string{"/bin/consul"}
	cmd := []string{
		"connect", "envoy",
		"-sidecar-for", serviceID,
		"-admin-bind", "127.0.0.1:19000",
		// Make /ready reachable from consul_node over the docker network
		"-envoy-ready-bind-address", "0.0.0.0",
		"-envoy-ready-bind-port", "19100",
		"-grpc-addr", grpcAddr,
		"-http-addr", httpAddr,
	}
	if cfg.SidecarGrpcTLS && cfg.SidecarCAPath != "" {
		cmd = append(cmd, "-grpc-ca-file", cfg.SidecarCAPath)
	}

	config := map[string]interface{}{
		"Image":      cfg.SidecarImage,
		"Entrypoint": entrypoint,
		"Cmd":        cmd,
		"Env": []string{
			"SERVICE_NAME=" + name,
			"CONSUL_HTTP_ADDR=" + httpAddr,
			"CONSUL_GRPC_ADDR=" + grpcAddr,
		},
		"HostConfig": map[string]interface{}{
			"NetworkMode":   "container:" + parentID,
			"RestartPolicy": map[string]string{"Name": "unless-stopped"},
		},
		"Labels": map[string]string{
			"consul-registrator": "sidecar",
			"service-id":         serviceID,
		},
	}

	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(config); err != nil {
		return err
	}
	log.Printf("creating sidecar container name=%s with config:\n%s", containerName, buf.String())

	r, err := d.client.Post("http://unix/containers/create?name="+containerName, "application/json", buf)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	// 409 = name already exists. Treat as idempotent and ensure it is started.
	if r.StatusCode == 409 {
		return d.StartContainer(ctx, containerName)
	}
	if r.StatusCode >= 400 {
		return fmt.Errorf("create failed: %s", r.Status)
	}

	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
		return err
	}

	return d.StartContainer(ctx, created.ID)
}

func (d *DockerClient) RemoveContainer(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", "http://unix/containers/"+id+"?force=true", nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to remove container %s: %s", id, resp.Status)
	}
	return nil
}
