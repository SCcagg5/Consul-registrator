package main

import (
	"context"
	"log"
	"strings"
	"time"
)

/// Agent reconciles Docker containers with Consul services.
type Agent struct {
	docker    *DockerClient
	consul    *ConsulClient
	metrics   *Metrics
	state     *State
	statePath string
	agentID   string
}

/// NewAgent creates an agent instance.
func NewAgent(d *DockerClient, c *ConsulClient, m *Metrics, s *State, statePath string, agentID string) *Agent {
	return &Agent{
		docker:    d,
		consul:    c,
		metrics:   m,
		state:     s,
		statePath: statePath,
		agentID:   agentID,
	}
}

/// Run executes a single reconciliation loop.
func (a *Agent) Run(ctx context.Context) error {
	containers, err := a.docker.ListContainers(ctx)
	if err != nil {
		a.metrics.Errors.Inc()
		return err
	}

	log.Printf("reconcile start containers=%d", len(containers))
	a.metrics.Containers.Set(float64(len(containers)))

	for _, c := range containers {
		insp, err := a.docker.Inspect(ctx, c.ID)
		if err != nil {
			a.metrics.Errors.Inc()
			log.Printf("container=%s inspect failed error=%v", c.ID[:12], err)
			continue
		}

		for k, v := range insp.Config.Labels {
			if k != "consul.service" && !strings.HasPrefix(k, "consul.service.") {
				continue
			}

			log.Printf("container=%s detected consul label key=%s", insp.ID[:12], k)

			svc, err := ParseServiceHCL(v)
			if err != nil {
				log.Printf("container=%s label=%s invalid HCL error=%v", insp.ID[:12], k, err)
				continue
			}

			name, ok := svc["name"].(string)
			if !ok || name == "" {
				log.Printf("container=%s label=%s missing service.name", insp.ID[:12], k)
				continue
			}

			id := insp.ID + ":" + name
			svc["id"] = id

			svc["meta"] = mergeMeta(svc["meta"], map[string]string{
				"managed-by":         "consul-registrator",
				"agent-id":           a.agentID,
				"docker-container-id": insp.ID,
			})

			log.Printf("container=%s registering service name=%s id=%s", insp.ID[:12], name, id)

			err = a.consul.RegisterService(ctx, svc)
			if err != nil {
				a.metrics.Errors.Inc()
				log.Printf("container=%s service=%s registration failed error=%v", insp.ID[:12], name, err)
				continue
			}

			a.state.Services[id] = true
			log.Printf("container=%s service=%s registered successfully", insp.ID[:12], name)
		}
	}

	a.metrics.Services.Set(float64(len(a.state.Services)))
	log.Printf("reconcile complete services=%d", len(a.state.Services))

	return SaveState(a.statePath, a.state)
}

/// RunOnce runs a single reconciliation cycle and exits.
func (a *Agent) RunOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.Run(ctx)
}

/// CleanLoop periodically removes orphaned services owned by this agent.
func (a *Agent) CleanLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			_ = a.CleanOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

/// CleanOnce removes Consul services that are explicitly owned by this agent and whose Docker container no longer exists.
func (a *Agent) CleanOnce(ctx context.Context) error {
	services, err := a.consul.AgentServices(ctx)
	if err != nil {
		a.metrics.Errors.Inc()
		log.Printf("clean agent/services failed error=%v", err)
		return err
	}

	removed := 0

	for _, s := range services {
		meta := s.Meta
		if meta == nil {
			continue
		}
		if meta["managed-by"] != "consul-registrator" {
			continue
		}
		if meta["agent-id"] != a.agentID {
			continue
		}

		cid := meta["docker-container-id"]
		if cid == "" {
			continue
		}

		exists, err := a.docker.ContainerExists(ctx, cid)
		if err != nil {
			a.metrics.Errors.Inc()
			log.Printf("clean docker inspect failed container=%s error=%v", cid, err)
			continue
		}
		if exists {
			continue
		}

		log.Printf("clean deregister service_id=%s container=%s", s.ID, cid)

		err = a.consul.DeregisterService(ctx, s.ID, s.Namespace, s.Partition)
		if err != nil {
			a.metrics.Errors.Inc()
			log.Printf("clean deregister failed service_id=%s error=%v", s.ID, err)
			continue
		}

		removed++
	}

	if removed > 0 {
		log.Printf("clean complete removed=%d", removed)
	}

	return nil
}

func mergeMeta(existing any, add map[string]string) map[string]string {
	out := map[string]string{}

	if m, ok := existing.(map[string]string); ok {
		for k, v := range m {
			out[k] = v
		}
	} else if m2, ok := existing.(map[string]any); ok {
		for k, v := range m2 {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
	}

	for k, v := range add {
		out[k] = v
	}

	return out
}
