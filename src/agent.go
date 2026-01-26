package main

import (
	"context"
	"time"
)

/// Agent reconciles Docker containers with Consul services.
type Agent struct {
	docker  *DockerClient
	consul  *ConsulClient
	metrics *Metrics
	state   *State
	statePath string
}

/// NewAgent creates an agent instance.
func NewAgent(d *DockerClient, c *ConsulClient, m *Metrics, s *State, statePath string) *Agent {
	return &Agent{
		docker: d,
		consul: c,
		metrics: m,
		state: s,
		statePath: statePath,
	}
}

/// Run executes a single reconciliation loop.
func (a *Agent) Run(ctx context.Context) error {
	containers, err := a.docker.ListContainers(ctx)
	if err != nil {
		a.metrics.Errors.Inc()
		return err
	}

	a.metrics.Containers.Set(float64(len(containers)))

	for _, c := range containers {
		insp, err := a.docker.Inspect(ctx, c.ID)
		if err != nil {
			a.metrics.Errors.Inc()
			continue
		}

		for _, v := range insp.Config.Labels {
			svc, err := ParseServiceHCL(v)
			if err != nil {
				continue
			}

			id := insp.ID + ":" + svc["name"].(string)
			svc["id"] = id

			err = a.consul.RegisterService(ctx, svc)
			if err == nil {
				a.state.Services[id] = true
			}
		}
	}

	a.metrics.Services.Set(float64(len(a.state.Services)))
	return SaveState(a.statePath, a.state)
}

/// RunOnce runs a single reconciliation cycle and exits.
func (a *Agent) RunOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.Run(ctx)
}
