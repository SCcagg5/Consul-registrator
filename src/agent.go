package main

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"
)

type Agent struct {
	docker    *DockerClient
	consul    *ConsulClient
	metrics   *Metrics
	state     *State
	statePath string
	cfg       *Config
}

func NewAgent(d *DockerClient, c *ConsulClient, m *Metrics, s *State, statePath string, cfg *Config) *Agent {
	return &Agent{docker: d, consul: c, metrics: m, state: s, statePath: statePath, cfg: cfg}
}

func (a *Agent) RunOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.Run(ctx)
}

func (a *Agent) Run(ctx context.Context) error {
	containers, err := a.docker.ListContainers(ctx)
	if err != nil {
		a.metrics.Errors.Inc()
		return err
	}
	a.metrics.Containers.Set(float64(len(containers)))
	log.Printf("reconcile start containers=%d", len(containers))

	// Index already-created sidecars so reconcile is idempotent.
	sidecarsByServiceID := map[string]DockerContainer{}
	for _, c := range containers {
		if c.Labels["consul-registrator"] != "sidecar" {
			continue
		}
		if sid := c.Labels["service-id"]; sid != "" {
			sidecarsByServiceID[sid] = c
		}
	}

	found := map[string]bool{}

	for _, c := range containers {
		insp, err := a.docker.Inspect(ctx, c.ID)
		if err != nil {
			a.metrics.Errors.Inc()
			continue
		}

		// Never reconcile sidecar containers as "services".
		if c.Labels["consul-registrator"] == "sidecar" {
			continue
		}

		var keys []string
		for k := range insp.Config.Labels {
			if strings.HasPrefix(k, "consul.service.") {
				keys = append(keys, k)
			} else if k == "consul.service" {
				log.Printf("container=%s label 'consul.service' is not supported, must use 'consul.service.<name>'", insp.ID)
			}
		}
		sort.Strings(keys)

		for _, k := range keys {
			labelName := strings.TrimPrefix(k, "consul.service.")
			svc, err := ParseServiceHCL(insp.Config.Labels[k])
			if err != nil {
				log.Printf("container=%s failed to parse label=%s error=%v", insp.ID, k, err)
				continue
			}

			svcName, ok := svc["name"].(string)
			if !ok || svcName == "" || svcName != labelName {
				log.Printf("container=%s invalid/mismatched service.name=%q for label=%q", insp.ID, svcName, labelName)
				continue
			}

			serviceID := insp.ID + ":" + svcName
			svc["id"] = serviceID

			// Apply sidecar defaults/auto behavior (checks + prometheus config) if connect.sidecar_service exists.
			applySidecarAutoAndDefaults(svc, svcName, serviceID, insp, a.cfg)

			err = a.consul.RegisterService(ctx, svc)
			if err != nil {
				log.Printf("container=%s failed to register service=%s error=%v", insp.ID, svcName, err)
				continue
			}

			a.state.Services[serviceID] = true
			found[serviceID] = true
			log.Printf("container=%s registered service=%s id=%s", insp.ID, svcName, serviceID)

			// Sidecar requested by label?
			sidecarKey := "consul.sidecar." + labelName
			if _, ok := insp.Config.Labels[sidecarKey]; !ok {
				continue
			}

			if !a.cfg.SidecarEnabled {
				log.Printf("container=%s sidecar requested but SIDECAR_ENABLED=false", insp.ID)
				continue
			}
			if a.cfg.SidecarImage == "" || a.cfg.SidecarGrpcAddr == "" || a.cfg.SidecarHttpAddr == "" {
				log.Printf("container=%s missing required sidecar config SIDECAR_IMAGE or GRPC/HTTP", insp.ID)
				continue
			}

			// Already created?
			if sc, ok := sidecarsByServiceID[serviceID]; ok {
				if sc.State != "running" {
					_ = a.docker.StartContainer(ctx, sc.ID) // best-effort
				}
				continue
			}

			launchErr := a.docker.LaunchSidecar(ctx, insp.ID, labelName, serviceID, a.cfg)
			if launchErr != nil {
				log.Printf("container=%s sidecar failed: %v", insp.ID, launchErr)
			} else {
				log.Printf("container=%s sidecar launched for service=%s", insp.ID, labelName)
			}
		}
	}

	// Deregister stale services.
	for id := range a.state.Services {
		if !found[id] {
			_ = a.consul.DeregisterService(ctx, id, "", "")
			delete(a.state.Services, id)
			log.Printf("deregistered stale service id=%s", id)
		}
	}

	// Remove orphan sidecar containers.
	for sid, sc := range sidecarsByServiceID {
		if !found[sid] {
			log.Printf("removing orphan sidecar container id=%s service-id=%s", sc.ID, sid)
			_ = a.docker.RemoveContainer(ctx, sc.ID)
		}
	}

	log.Printf("reconcile complete services=%d", len(a.state.Services))
	return SaveState(a.statePath, a.state)
}

// applySidecarAutoAndDefaults does 2 things:
//
// 1) If connect.sidecar_service.auto == true, it injects safe default checks so Consul doesn't add
//    the default "tcp 127.0.0.1:<port>" check (which breaks when the agent and proxy are in different netns).
//
// 2) It injects proxy.config.envoy_prometheus_bind_addr by default so Envoy exposes a Prometheus scrape endpoint.
func applySidecarAutoAndDefaults(svc map[string]any, serviceName, serviceID string, insp *DockerInspect, cfg *Config) {
	connect, ok := svc["connect"].(map[string]any)
	if !ok {
		return
	}
	sidecar, ok := connect["sidecar_service"].(map[string]any)
	if !ok {
		return
	}

	// "auto" is a custom flag consumed by this registrator (not a Consul API field).
	auto := false
	if v, ok := sidecar["auto"].(bool); ok {
		auto = v
		delete(sidecar, "auto")
	} else if v, ok := sidecar["Auto"].(bool); ok {
		auto = v
		delete(sidecar, "Auto")
	}

	// Always normalize existing checks (supports snake_case from HCL) and rewrite AliasService placeholders.
	normalizeSidecarChecks(sidecar, serviceName, serviceID)

	// Enable Envoy Prometheus endpoint by default (one per container/netns).
	if cfg != nil && cfg.SidecarPrometheusBindAddr != "" {
		ensureEnvoyPrometheus(sidecar, cfg.SidecarPrometheusBindAddr)
	}

	if !auto {
		return
	}

	host := dockerResolvableName(insp, serviceName)
	if host == "" {
		host = containerIP(insp)
	}
	if host == "" {
		return
	}

	readyURL := "http://" + host + ":19100/ready"

	checks := getChecksSlice(sidecar)

	if !hasReadyCheck(checks) {
		checks = append(checks, map[string]any{
			"Name":     "Envoy Ready",
			"HTTP":     readyURL,
			"Interval": "10s",
		})
	}

	if !hasAliasCheck(checks) {
		checks = append(checks, map[string]any{
			"Name":         "Connect Sidecar Aliasing " + serviceName,
			"AliasService": serviceID,
		})
	}

	// Re-normalize after injection (ensures any placeholders are rewritten).
	sidecar["checks"] = checks
	normalizeSidecarChecks(sidecar, serviceName, serviceID)
}

func dockerResolvableName(insp *DockerInspect, fallback string) string {
	// Docker inspect Name is like "/api" or "/project-api-1"
	if insp != nil && insp.Name != "" {
		n := strings.TrimPrefix(insp.Name, "/")
		if n != "" {
			return n
		}
	}
	return fallback
}

func containerIP(insp *DockerInspect) string {
	if insp == nil {
		return ""
	}
	for _, n := range insp.NetworkSettings.Networks {
		if n.IPAddress != "" {
			return n.IPAddress
		}
	}
	return ""
}

func ensureEnvoyPrometheus(sidecar map[string]any, bindAddr string) {
	proxy, _ := sidecar["proxy"].(map[string]any)
	if proxy == nil {
		proxy = map[string]any{}
		sidecar["proxy"] = proxy
	}
	cfg, _ := proxy["config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
		proxy["config"] = cfg
	}
	if _, ok := cfg["envoy_prometheus_bind_addr"]; !ok {
		cfg["envoy_prometheus_bind_addr"] = bindAddr
	}
}

func getChecksSlice(sidecar map[string]any) []any {
	if raw, ok := sidecar["checks"].([]any); ok {
		return raw
	}
	// Some users may define a single "check" object instead.
	if one, ok := sidecar["check"].(map[string]any); ok {
		return []any{one}
	}
	return []any{}
}

func normalizeSidecarChecks(sidecar map[string]any, serviceName, serviceID string) {
	// checks: []map
	if raw, ok := sidecar["checks"].([]any); ok {
		for i, c := range raw {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			raw[i] = normalizeOneCheck(m, serviceName, serviceID)
		}
		sidecar["checks"] = raw
	}

	// check: map
	if one, ok := sidecar["check"].(map[string]any); ok {
		sidecar["check"] = normalizeOneCheck(one, serviceName, serviceID)
	}
}

func normalizeOneCheck(m map[string]any, serviceName, serviceID string) map[string]any {
	rename := func(oldKey, newKey string) {
		if v, ok := m[oldKey]; ok {
			if _, exists := m[newKey]; !exists {
				m[newKey] = v
			}
			delete(m, oldKey)
		}
	}

	// Common snake_case -> Consul API casing
	rename("name", "Name")
	rename("http", "HTTP")
	rename("tcp", "TCP")
	rename("udp", "UDP")
	rename("interval", "Interval")
	rename("timeout", "Timeout")
	rename("alias_service", "AliasService")
	rename("alias_node", "AliasNode")

	// AliasService should point to the parent service ID (not just the service name).
	if v, ok := m["AliasService"].(string); ok {
		if v == "" || v == serviceName || v == "$SERVICE_ID" || v == "${SERVICE_ID}" {
			m["AliasService"] = serviceID
		}
	}

	return m
}

func hasReadyCheck(checks []any) bool {
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := m["HTTP"].(string); ok && strings.Contains(v, "/ready") {
			return true
		}
		if v, ok := m["http"].(string); ok && strings.Contains(v, "/ready") {
			return true
		}
	}
	return false
}

func hasAliasCheck(checks []any) bool {
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := m["AliasService"]; ok {
			return true
		}
		if _, ok := m["alias_service"]; ok {
			return true
		}
	}
	return false
}
