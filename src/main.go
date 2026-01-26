package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"
)

/// main is the entry point of the Consul registrator agent.
func main() {
	consulAddr := requireEnv("CONSUL_HTTP_ADDR")
	consulToken := envOr("CONSUL_HTTP_TOKEN", "")
	dockerSock := envOr("DOCKER_SOCK", "/var/run/docker.sock")
	statePath := envOr("STATE_PATH", "/var/lib/docker-consul-agent/state.json")
	metricsAddr := envOr("METRICS_ADDR", ":9090")

	agentID := envOr("AGENT_ID", "")
	cleanIntervalStr := envOr("CLEAN_INTERVAL", "30s")

	dryRunFlag := flag.Bool("dry-run", false, "dry run mode")
	onceFlag := flag.Bool("once", false, "run a single reconciliation cycle")
	healthcheckFlag := flag.Bool("healthcheck", false, "healthcheck mode")
	flag.Parse()

	dryRun := *dryRunFlag || envBool("DRY_RUN")

	if agentID == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			log.Fatal("AGENT_ID is required when hostname is unavailable")
		}
		agentID = h
	}

	cleanInterval, err := time.ParseDuration(cleanIntervalStr)
	if err != nil || cleanInterval <= 0 {
		log.Fatal("invalid CLEAN_INTERVAL")
	}

	if *healthcheckFlag {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		docker := NewDockerClient(dockerSock, 2*time.Second)
		_, derr := docker.ListContainers(ctx)
		if derr != nil {
			os.Exit(1)
		}

		consul := NewConsulClient(consulAddr, consulToken, 2*time.Second, false)
		cerr := consul.PassCheck(ctx, "consul-registrator-healthcheck", "", "healthcheck")
		if cerr != nil {
			os.Exit(1)
		}

		os.Exit(0)
	}

	metrics := NewMetrics()
	ServeMetrics(metricsAddr)

	docker := NewDockerClient(dockerSock, 10*time.Second)
	consul := NewConsulClient(consulAddr, consulToken, 10*time.Second, dryRun)

	state, _ := LoadState(statePath)

	agent := NewAgent(docker, consul, metrics, state, statePath, agentID)

	if *onceFlag {
		_ = agent.RunOnce()
		return
	}

	ctx := context.Background()
	go agent.CleanLoop(ctx, cleanInterval)

	for {
		_ = agent.RunOnce()
		time.Sleep(10 * time.Second)
	}
}
