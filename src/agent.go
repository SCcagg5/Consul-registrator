package main

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"
	"encoding/json"
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

	// Index already created sidecars so reconcile is idempotent
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

		// Skip sidecar containers
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

			if _, hasAddress := svc["address"]; !hasAddress {
				if _, hasAddress := svc["Address"]; !hasAddress {
					addr := resolveServiceAddress(insp, svcName)
					if addr != "" {
						svc["address"] = addr
					}
				}
			}

			sidecarKey := "consul.sidecar." + labelName
			_, sidecarRequested := insp.Config.Labels[sidecarKey]

			// Apply: auto=true => add checks; always normalize AliasService; optionally inject prometheus config
			applySidecarAutoAndProm(svc, svcName, serviceID, a.cfg, sidecarRequested)
			
			b, _ := json.MarshalIndent(svc, "", "  ")
			log.Printf("REGISTER PAYLOAD:\n%s", string(b))

			err = a.consul.RegisterService(ctx, svc)
			if err != nil {
				log.Printf("container=%s failed to register service=%s error=%v", insp.ID, svcName, err)
				continue
			}

			a.state.Services[serviceID] = true
			found[serviceID] = true
			log.Printf("container=%s registered service=%s id=%s", insp.ID, svcName, serviceID)

			// Launch sidecar container if requested
			if sidecarRequested {
				if !a.cfg.SidecarEnabled {
					log.Printf("container=%s sidecar requested but SIDECAR_ENABLED=false", insp.ID)
					continue
				}
				if a.cfg.SidecarImage == "" || a.cfg.SidecarGrpcAddr == "" || a.cfg.SidecarHttpAddr == "" {
					log.Printf("container=%s missing required sidecar config SIDECAR_IMAGE or GRPC/HTTP", insp.ID)
					continue
				}

				// already exists?
				if sc, ok := sidecarsByServiceID[serviceID]; ok {
					if sc.State != "running" {
						_ = a.docker.StartContainer(ctx, sc.ID) // best-effort
					}
					continue
				}

				needsNetAdmin := sidecarNeedsTransparentProxy(svc)
				launchErr := a.docker.LaunchSidecar(ctx, insp.ID, labelName, serviceID, a.cfg, needsNetAdmin)
				if launchErr != nil {
					log.Printf("container=%s sidecar failed: %v", insp.ID, launchErr)
				} else {
					log.Printf("container=%s sidecar launched for service=%s", insp.ID, labelName)
				}
			}
		}
	}

	// Deregister stale services
	for id := range a.state.Services {
		if !found[id] {
			_ = a.consul.DeregisterService(ctx, id, "", "")
			delete(a.state.Services, id)
			log.Printf("deregistered stale service id=%s", id)
		}
	}

	// Remove orphan sidecars (service-id not present anymore)
	for sid, sc := range sidecarsByServiceID {
		if !found[sid] {
			log.Printf("removing orphan sidecar container id=%s service-id=%s", sc.ID, sid)
			_ = a.docker.RemoveContainer(ctx, sc.ID)
		}
	}

	log.Printf("reconcile complete services=%d", len(a.state.Services))
	return SaveState(a.statePath, a.state)
}

func applySidecarAutoAndProm(svc map[string]any, serviceName, serviceID string, cfg *Config, sidecarRequested bool) {
	connect, ok := svc["connect"].(map[string]any)
	if !ok {
		return
	}
	sidecar, ok := connect["sidecar_service"].(map[string]any)
	if !ok {
		return
	}

	// read + delete custom auto flag (not a Consul field)
	auto := false
	if v, ok := sidecar["auto"]; ok {
		auto = boolFromAny(v)
		delete(sidecar, "auto")
	}
	if v, ok := sidecar["Auto"]; ok {
		auto = boolFromAny(v)
		delete(sidecar, "Auto")
	}

	// Normalize existing checks + fix AliasService to point to serviceID
	checks := extractChecks(sidecar)
	hasReady := false
	hasAlias := false

	for i := range checks {
		m, ok := checks[i].(map[string]any)
		if !ok {
			continue
		}
		normalizeCheckKeys(m)
		rewriteAliasService(m, serviceName, serviceID)

		if v, ok := m["HTTP"].(string); ok && strings.Contains(v, "/ready") {
			hasReady = true
		}
		if v, ok := m["AliasService"].(string); ok && v != "" {
			hasAlias = true
		}
	}

	// If auto=true: ensure required checks exist (and override default Consul TCP 127.0.0.1 check behavior)
	if auto {
		if !hasReady {
			checks = append(checks, map[string]any{
				"Name":     "Envoy Ready",
				"HTTP":     "http://" + serviceName + ":19100/ready",
				"Interval": "10s",
			})
			checks = append(checks, map[string]any{
				"Name":     "Envoy Ready",
				"HTTP":     "http://" + serviceName + ":19100/ready",
				"Interval": "10s",
			})

			checks := []map[string]any{
		{
			"Name":     "Envoy Ready",
			"HTTP":     fmt.Sprintf("http://%s:19100/ready", checkHost),
			"Method":   "GET",
			"Interval": "10s",
			"Timeout":  "2s",
			// optionnel: évite le “passing” trop tôt
			"SuccessBeforePassing": 2,
		},
		{
			"Name":     "Envoy Metrics",
			"HTTP":     fmt.Sprintf("http://%s:%d%s", checkHost, metricsPort, metricsPath), // default /metrics
			"Method":   "GET",
			"Interval": "30s",
			"Timeout":  "2s",
			"SuccessBeforePassing": 2,
		},
		}

		}
		if !hasAlias {
			checks = append(checks, map[string]any{
				"Name":         "Connect Sidecar Aliasing " + serviceName,
				"AliasService": serviceID,
			})
		}

		ensureTransparentProxy(sidecar)
	}

	// Write back checks in Consul API format
	if len(checks) > 0 {
		delete(sidecar, "check")
		sidecar["checks"] = checks
	}

	// Inject Envoy Prometheus endpoint by default (only when sidecar is actually requested)
	if sidecarRequested && cfg != nil && cfg.SidecarPrometheusBindAddr != "" {
		ensureEnvoyPrometheus(sidecar, cfg.SidecarPrometheusBindAddr)
	}
}

func boolFromAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "1", "true", "yes", "y", "on":
			return true
		}
		return false
	default:
		return false
	}
}

func extractChecks(sidecar map[string]any) []any {
	if raw, ok := sidecar["checks"].([]any); ok {
		return raw
	}
	if one, ok := sidecar["check"].(map[string]any); ok {
		return []any{one}
	}
	return []any{}
}

func normalizeCheckKeys(m map[string]any) {
	rename := func(oldKey, newKey string) {
		if v, ok := m[oldKey]; ok {
			if _, exists := m[newKey]; !exists {
				m[newKey] = v
			}
			delete(m, oldKey)
		}
	}

	rename("name", "Name")
	rename("http", "HTTP")
	rename("tcp", "TCP")
	rename("udp", "UDP")
	rename("interval", "Interval")
	rename("timeout", "Timeout")
	rename("alias_service", "AliasService")
	rename("alias_node", "AliasNode")
}

func rewriteAliasService(check map[string]any, serviceName, serviceID string) {
	if v, ok := check["AliasService"].(string); ok {
		if v == "" || v == serviceName || v == "$SERVICE_ID" || v == "${SERVICE_ID}" {
			check["AliasService"] = serviceID
		}
	}
	// if still snake_case
	if v, ok := check["alias_service"].(string); ok {
		if v == "" || v == serviceName || v == "$SERVICE_ID" || v == "${SERVICE_ID}" {
			check["alias_service"] = serviceID
		}
	}
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

func resolveServiceAddress(insp *DockerInspect, fallback string) string {
	// Prefer Docker container name (without leading "/")
	if insp != nil {
		name := strings.TrimPrefix(strings.TrimSpace(insp.Name), "/")
		if name != "" {
			return name
		}
	}

	// Fallback to provided name (usually svcName)
	if fallback != "" {
		return fallback
	}

	// Final fallback: first network IP
	if insp != nil {
		for _, net := range insp.NetworkSettings.Networks {
			if net.IPAddress != "" {
				return net.IPAddress
			}
		}
	}

	return ""
}

func ensureTransparentProxy(sidecar map[string]any) {
	proxy, _ := sidecar["proxy"].(map[string]any)
	if proxy == nil {
		proxy = map[string]any{}
		sidecar["proxy"] = proxy
	}

	if _, exists := proxy["TransparentProxy"]; !exists {
		proxy["TransparentProxy"] = map[string]any{
			"OutboundListenerPort": 15001,
			// "InboundListenerPort": 15101,
		}

	}
}

func sidecarNeedsTransparentProxy(svc map[string]any) bool {
	connect, ok := svc["connect"].(map[string]any)
	if !ok {
		return false
	}
	sidecar, ok := connect["sidecar_service"].(map[string]any)
	if !ok {
		return false
	}
	proxy, ok := sidecar["proxy"].(map[string]any)
	if !ok {
		return false
	}
	_, hasTP := proxy["TransparentProxy"]
	return hasTP
}

