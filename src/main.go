package main

import (
	"context"
	"flag"
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

	dryRunFlag := flag.Bool("dry-run", false, "dry run mode")
	onceFlag := flag.Bool("once", false, "run a single reconciliation cycle")
	healthcheckFlag := flag.Bool("healthcheck", false, "healthcheck mode")
	flag.Parse()

	dryRun := *dryRunFlag || envBool("DRY_RUN")

	if *healthcheckFlag {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		docker := NewDockerClient(dockerSock, 2*time.Second)

		_, err := docker.ListContainers(ctx)
		if err != nil {
			os.Exit(1)
		}

		consul := NewConsulClient(
			consulAddr,
			consulToken,
			2*time.Second,
			false,
		)

		err = consul.PassCheck(
			ctx,
			"consul-registrator-healthcheck",
			"",
			"healthcheck",
		)

		if err != nil {
			os.Exit(1)
		}

		os.Exit(0)
	}

	metrics := NewMetrics()
	ServeMetrics(metricsAddr)

	docker := NewDockerClient(dockerSock, 10*time.Second)
	consul := NewConsulClient(consulAddr, consulToken, 10*time.Second, dryRun)

	state, _ := LoadState(statePath)

	agent := NewAgent(docker, consul, metrics, state, statePath)

	if *onceFlag {
		_ = agent.RunOnce()
		return
	}

	for {
		_ = agent.RunOnce()
		time.Sleep(10 * time.Second)
	}
}
