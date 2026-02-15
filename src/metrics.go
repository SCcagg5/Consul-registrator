package main

import (
	"net/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	Containers        prometheus.Gauge
	Services          prometheus.Gauge
	TTLChecks         prometheus.Gauge
	Events            prometheus.Counter
	Errors            prometheus.Counter
	SidecarsLaunched  prometheus.Gauge
	SidecarsDeleted   prometheus.Gauge
}

func NewMetrics() *Metrics {
	m := &Metrics{
		Containers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dockconsul_containers_total",
			Help: "Number of Docker containers observed",
		}),
		Services: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dockconsul_services_registered_total",
			Help: "Number of Consul services registered",
		}),
		TTLChecks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dockconsul_ttl_checks_total",
			Help: "Number of active TTL checks",
		}),
		Events: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dockconsul_events_total",
			Help: "Number of Docker events processed",
		}),
		Errors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dockconsul_errors_total",
			Help: "Number of errors encountered",
		}),
		SidecarsLaunched: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dockconsul_sidecars_launched",
			Help: "Number of sidecar containers launched in last cycle",
		}),
		SidecarsDeleted: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dockconsul_sidecars_deleted",
			Help: "Number of orphan sidecar containers deleted in last cycle",
		}),
	}

	prometheus.MustRegister(
		m.Containers,
		m.Services,
		m.TTLChecks,
		m.Events,
		m.Errors,
		m.SidecarsLaunched,
		m.SidecarsDeleted,
	)
	return m
}

func ServeMetrics(addr string) {
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(addr, nil)
}
