package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultReRegisterInterval = 5 * time.Minute

type Agent struct {
	docker    *DockerClient
	consul    *ConsulClient
	metrics   *Metrics
	state     *State
	statePath string
	cfg       *Config

	servicePayloadHash map[string]string
	lastRegisterAt      map[string]time.Time
}

func NewAgent(d *DockerClient, c *ConsulClient, m *Metrics, s *State, statePath string, cfg *Config) *Agent {
	return &Agent{
		docker:              d,
		consul:              c,
		metrics:             m,
		state:               s,
		statePath:           statePath,
		cfg:                 cfg,
		servicePayloadHash:  map[string]string{},
		lastRegisterAt:      map[string]time.Time{},
	}
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
			applySidecarAutoAndProm(svc, svcName, serviceID, a.cfg, sidecarRequested)
			applyAutoTCPCheckOnServiceOrEnvoy(svc, svcName)
			found[serviceID] = true
			payloadHash := hashServicePayload(svc)

			shouldRegister := false
			if !a.state.Services[serviceID] {
				shouldRegister = true
			} else if prev, ok := a.servicePayloadHash[serviceID]; !ok || prev != payloadHash {
				shouldRegister = true
			} else {
				last := a.lastRegisterAt[serviceID]
				if time.Since(last) >= defaultReRegisterInterval {
					shouldRegister = true
				}
			}

			if shouldRegister {
				b, _ := json.MarshalIndent(svc, "", "  ")
				log.Printf("REGISTER PAYLOAD:\n%s", string(b))

				err = a.consul.RegisterService(ctx, svc)
				if err != nil {
					log.Printf("container=%s failed to register service=%s error=%v", insp.ID, svcName, err)
					continue
				}

				a.servicePayloadHash[serviceID] = payloadHash
				a.lastRegisterAt[serviceID] = time.Now()
				a.state.Services[serviceID] = true
				log.Printf("container=%s registered service=%s id=%s", insp.ID, svcName, serviceID)
			} else {
				a.state.Services[serviceID] = true
			}

			if sidecarRequested {
				if !a.cfg.SidecarEnabled {
					log.Printf("container=%s sidecar requested but SIDECAR_ENABLED=false", insp.ID)
					continue
				}
				if a.cfg.SidecarImage == "" || a.cfg.SidecarGrpcAddr == "" || a.cfg.SidecarHttpAddr == "" {
					log.Printf("container=%s missing required sidecar config SIDECAR_IMAGE or GRPC/HTTP", insp.ID)
					continue
				}

				if sc, ok := sidecarsByServiceID[serviceID]; ok {
					if sc.State != "running" {
						_ = a.docker.StartContainer(ctx, sc.ID)
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

	for id := range a.state.Services {
		if !found[id] {
			_ = a.consul.DeregisterService(ctx, id, "", "")
			delete(a.state.Services, id)
			delete(a.servicePayloadHash, id)
			delete(a.lastRegisterAt, id)
			log.Printf("deregistered stale service id=%s", id)
		}
	}

	for sid, sc := range sidecarsByServiceID {
		if !found[sid] {
			log.Printf("removing orphan sidecar container id=%s service-id=%s", sc.ID, sid)
			_ = a.docker.RemoveContainer(ctx, sc.ID)
		}
	}

	log.Printf("reconcile complete services=%d", len(a.state.Services))
	return SaveState(a.statePath, a.state)
}

func hashServicePayload(svc map[string]any) string {
	b, err := json.Marshal(svc)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func parseHostPort(addr string) (string, int, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0, fmt.Errorf("empty bind addr")
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, err
	}
	if h == "" {
		h = "0.0.0.0"
	}
	return h, port, nil
}

func isValidPort(p int) bool { return p >= 1 && p <= 65535 }

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func isReservedSidecarPort(p int) bool {
	switch p {
	case 15000, 15001, 15002, 15090, 19000, 19100:
		return true
	default:
		return false
	}
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

	checkHost := serviceName
	if addr, ok := svc["address"].(string); ok && addr != "" {
		checkHost = addr
	}
	if addr, ok := svc["Address"].(string); ok && addr != "" {
		checkHost = addr
	}

	auto := false
	if v, ok := sidecar["auto"]; ok {
		auto = boolFromAny(v)
		delete(sidecar, "auto")
	}
	if v, ok := sidecar["Auto"]; ok {
		auto = boolFromAny(v)
		delete(sidecar, "Auto")
	}

	checks := extractChecks(sidecar)
	hasReady := false
	hasAlias := false
	hasMetrics := false

	for i := range checks {
		m, ok := checks[i].(map[string]any)
		if !ok {
			continue
		}
		normalizeCheckKeys(m)
		rewriteAliasService(m, serviceName, serviceID)

		if v, ok := m["HTTP"].(string); ok {
			if strings.Contains(v, "/ready") {
				hasReady = true
			}
			if strings.Contains(v, "/metrics") {
				hasMetrics = true
			}
		}
		if _, ok := m["TCP"].(string); ok {
			if name, _ := m["Name"].(string); strings.EqualFold(strings.TrimSpace(name), "Envoy Metrics") {
				hasMetrics = true
			}
		}
		if v, ok := m["AliasService"].(string); ok && v != "" {
			hasAlias = true
		}
	}

	if auto {
		if !hasReady {
			checks = append(checks, map[string]any{
				"Name":     "Envoy Ready",
				"HTTP":     "http://" + checkHost + ":19100/ready",
				"Interval": "10s",
				"Timeout":  "2s",
			})
		}

		if !hasMetrics && sidecarRequested && cfg != nil && strings.TrimSpace(cfg.SidecarPrometheusBindAddr) != "" {
			host, port, err := parseHostPort(cfg.SidecarPrometheusBindAddr)
			if err != nil {
				log.Printf("service=%s invalid SIDECAR_PROMETHEUS_BIND_ADDR=%q (skip metrics check): %v", serviceName, cfg.SidecarPrometheusBindAddr, err)
			} else if !isValidPort(port) {
				log.Printf("service=%s invalid metrics port=%d from SIDECAR_PROMETHEUS_BIND_ADDR=%q (skip metrics check)", serviceName, port, cfg.SidecarPrometheusBindAddr)
			} else if isLoopbackHost(host) {
				log.Printf("service=%s metrics bind addr=%q is loopback (skip metrics check; not reachable by Consul)", serviceName, cfg.SidecarPrometheusBindAddr)
			} else if isReservedSidecarPort(port) {
				log.Printf("service=%s metrics port=%d collides with reserved ports (skip metrics check) bind=%q", serviceName, port, cfg.SidecarPrometheusBindAddr)
			} else {
				checks = append(checks, map[string]any{
					"Name":     "Envoy Metrics",
					"TCP":      fmt.Sprintf("%s:%d", checkHost, port),
					"Interval": "30s",
					"Timeout":  "2s",
				})
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

	if len(checks) > 0 {
		delete(sidecar, "check")
		sidecar["checks"] = checks
	}

	if sidecarRequested && cfg != nil && strings.TrimSpace(cfg.SidecarPrometheusBindAddr) != "" {
		host, port, err := parseHostPort(cfg.SidecarPrometheusBindAddr)
		if err != nil {
			log.Printf("service=%s skip envoy_prometheus_bind_addr=%q (invalid host:port): %v", serviceName, cfg.SidecarPrometheusBindAddr, err)
		} else if !isValidPort(port) {
			log.Printf("service=%s skip envoy_prometheus_bind_addr=%q (invalid port=%d)", serviceName, cfg.SidecarPrometheusBindAddr, port)
		} else if isLoopbackHost(host) {
			log.Printf("service=%s skip envoy_prometheus_bind_addr=%q (loopback; not reachable by Consul)", serviceName, cfg.SidecarPrometheusBindAddr)
		} else if isReservedSidecarPort(port) {
			log.Printf("service=%s skip envoy_prometheus_bind_addr=%q (reserved port=%d)", serviceName, cfg.SidecarPrometheusBindAddr, port)
		} else {
			ensureEnvoyPrometheus(sidecar, cfg.SidecarPrometheusBindAddr)
		}
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
	if insp != nil {
		name := strings.TrimPrefix(strings.TrimSpace(insp.Name), "/")
		if name != "" {
			return name
		}
	}

	if fallback != "" {
		return fallback
	}

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

	if v, exists := proxy["TransparentProxy"]; exists {
		if _, ok := proxy["transparent_proxy"]; !ok {
			proxy["transparent_proxy"] = v
		}
		delete(proxy, "TransparentProxy")
	}

	if _, exists := proxy["transparent_proxy"]; !exists {
		proxy["transparent_proxy"] = map[string]any{}
	}

	if tp, ok := proxy["transparent_proxy"].(map[string]any); ok {
		delete(tp, "inbound_listener_port")
		delete(tp, "outbound_listener_port")
		delete(tp, "InboundListenerPort")
		delete(tp, "OutboundListenerPort")
	}

	cfg, _ := proxy["config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
		proxy["config"] = cfg
	}

	if v, ok := cfg["bind_address"]; !ok || strings.TrimSpace(fmt.Sprint(v)) == "" {
		cfg["bind_address"] = "0.0.0.0"
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
	if _, hasTP := proxy["transparent_proxy"]; hasTP {
		return true
	}
	if _, hasTP := proxy["TransparentProxy"]; hasTP {
		return true
	}
	return false
}

func applyAutoTCPCheckOnServiceOrEnvoy(svc map[string]any, serviceName string) {
	host := serviceName
	if addr, ok := svc["address"].(string); ok && addr != "" {
		host = addr
	} else if addr, ok := svc["Address"].(string); ok && addr != "" {
		host = addr
	}

	checkPort := 0
	checkName := ""

	if sidecarNeedsTransparentProxy(svc) {
		checkPort = 15000
		checkName = "Envoy TP Listener " + serviceName
	} else {
		port := intFromAny(svc["port"])
		if port == 0 {
			port = intFromAny(svc["Port"])
		}
		if port < 1 || port > 65535 {
			return
		}
		if isReservedSidecarPort(port) {
			return
		}
		checkPort = port
		checkName = "Service TCP " + serviceName
	}

	checks := []any{}
	if raw, ok := svc["checks"].([]any); ok {
		checks = raw
	} else if one, ok := svc["check"].(map[string]any); ok {
		checks = []any{one}
	}

	targetSuffix := ":" + strconv.Itoa(checkPort)
	for _, c := range checks {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		normalizeCheckKeys(m)

		if v, ok := m["TCP"].(string); ok && strings.HasSuffix(v, targetSuffix) {
			return
		}

		if name, _ := m["Name"].(string); strings.EqualFold(strings.TrimSpace(name), checkName) {
			return
		}
	}

	checks = append(checks, map[string]any{
		"Name":     checkName,
		"TCP":      fmt.Sprintf("%s:%d", host, checkPort),
		"Interval": "10s",
		"Timeout":  "2s",
		"Status":                 "passing",
		"FailuresBeforeCritical": 6,
		"SuccessBeforePassing":   1,
	})

	delete(svc, "check")
	svc["checks"] = checks
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		p, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0
		}
		return p
	default:
		return 0
	}
}
