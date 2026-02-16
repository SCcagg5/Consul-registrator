package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	var (
		dockerSockEnv  = getenv("DOCKER_SOCKET", "/var/run/docker.sock")
		consulAddrEnv  = getenv("CONSUL_HTTP_ADDR", "http://localhost:8500")
		statePathEnv   = getenv("STATE_PATH", "/tmp/registrator-state.json")
		metricsAddrEnv = getenv("METRICS_ADDR", ":9090")
	)

	var (
		dockerSock       = flag.String("docker-socket", dockerSockEnv, "Docker socket path")
		consulAddr       = flag.String("consul-addr", consulAddrEnv, "Consul HTTP address")
		statePath        = flag.String("state", statePathEnv, "State file path")
		metricsAddr      = flag.String("metrics-addr", metricsAddrEnv, "Prometheus metrics address")
		onceFlag         = flag.Bool("once", false, "Run only one reconciliation loop")
		healthcheckFlag  = flag.Bool("healthcheck", false, "Exit 0 if registrator can reach Docker")
	)
	flag.Parse()

	if *healthcheckFlag {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		docker := NewDockerClient(*dockerSock, 2*time.Second)
		if _, err := docker.ListContainers(ctx); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	ServeMetrics(*metricsAddr)
	metrics := NewMetrics()
	docker := NewDockerClient(*dockerSock, 5*time.Second)
	consul := NewConsulClient(*consulAddr, "", 5*time.Second, false)
	state, _ := LoadState(*statePath)
	cfg := LoadConfig()

	agent := NewAgent(docker, consul, metrics, state, *statePath, cfg)

	if *onceFlag {
		_ = agent.RunOnce()
		return
	}

	for {
		_ = agent.RunOnce()
		time.Sleep(10 * time.Second)
	}
}

func getenv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

type Config struct {
	SidecarEnabled   bool
	SidecarImage     string
	SidecarHttpAddr  string
	SidecarGrpcAddr  string
	SidecarGrpcTLS   bool
	SidecarCAPath    string
	SidecarPrometheusBindAddr string
}

func LoadConfig() *Config {
	prom := os.Getenv("SIDECAR_PROMETHEUS_BIND_ADDR")
	prom = strings.TrimSpace(prom)
	switch strings.ToLower(prom) {
	case "", "0", "off", "false", "disabled":
		prom = ""
	}

	cfg := &Config{
		SidecarEnabled:            os.Getenv("SIDECAR_ENABLED") == "true",
		SidecarImage:              os.Getenv("SIDECAR_IMAGE"),
		SidecarHttpAddr:           os.Getenv("SIDECAR_CONSUL_HTTP"),
		SidecarGrpcAddr:           os.Getenv("SIDECAR_CONSUL_GRPC"),
		SidecarGrpcTLS:            os.Getenv("SIDECAR_GRPC_TLS") == "true",
		SidecarCAPath:             os.Getenv("SIDECAR_GRPC_CA_FILE"),
		SidecarPrometheusBindAddr: prom,
	}

	log.Printf("config: SIDECAR_ENABLED=%v", cfg.SidecarEnabled)
	log.Printf("config: SIDECAR_IMAGE=%q", cfg.SidecarImage)
	log.Printf("config: SIDECAR_CONSUL_HTTP=%q", cfg.SidecarHttpAddr)
	log.Printf("config: SIDECAR_CONSUL_GRPC=%q", cfg.SidecarGrpcAddr)
	log.Printf("config: SIDECAR_GRPC_TLS=%v", cfg.SidecarGrpcTLS)
	log.Printf("config: SIDECAR_GRPC_CA_FILE=%q", cfg.SidecarCAPath)
	log.Printf("config: SIDECAR_PROMETHEUS_BIND_ADDR=%q", cfg.SidecarPrometheusBindAddr)

	return cfg
}
