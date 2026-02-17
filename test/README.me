# Consul Registrator (Docker → Consul) + Optional Envoy Sidecar

A small Go “registrator” that watches Docker containers, reads `consul.service.<name>` labels (as **HCL**), then **registers / updates / deregisters** services in a **Consul Agent**.  
Optionally, it can **launch an Envoy sidecar** (via `consul connect envoy`) in a dedicated container, attached to the application container.

> TL;DR: you describe services via Docker labels; the agent reconciles them into Consul every 10 seconds.

---

## Features

- **Docker discovery** via the Docker API (Unix socket).
- Periodic **reconciliation** (polling every 10s):
  - Register service if new
  - Re-register if payload changes (hash) or every 5 minutes
  - Deregister if the service no longer exists in Docker
- **Service definition via HCL** in Docker labels:
  - `consul.service.<name>` (required)
  - `consul.sidecar.<name>` (optional; presence = sidecar requested)
- **Auto checks**
  - Automatically adds a TCP check if no equivalent check already exists
  - For connect/transparent proxy: check targets the Envoy listener
- **Connect / Envoy**
  - If `connect { sidecar_service { ... } }` is present in the service config and label `consul.sidecar.<name>` exists, the agent launches a sidecar.
  - Optional injection of “Envoy Ready” and “Envoy Metrics” checks, plus an Alias check.
- **Prometheus metrics** exposed at `/metrics`.

---

## Requirements

- Docker accessible via Unix socket (default `/var/run/docker.sock`).
- A **Consul Agent** reachable over HTTP (default `http://localhost:8500`).
- For the sidecar feature:
  - a `SIDECAR_IMAGE` that contains the `consul` CLI + `iptables` + `/bin/sh`
  - if transparent proxy is enabled: the sidecar needs `NET_ADMIN`

---

## Build

```bash
go build -o consul-registrator .
````

---

## Usage

### CLI

```bash
./consul-registrator \
  -docker-socket /var/run/docker.sock \
  -consul-addr http://localhost:8500 \
  -state /tmp/registrator-state.json \
  -metrics-addr :9090
```

Useful flags:

* `-once`: run a single reconciliation cycle and exit
* `-healthcheck`: exit 0 if Docker is reachable

### Environment variables (override defaults)

* `DOCKER_SOCKET` (default `/var/run/docker.sock`)
* `CONSUL_HTTP_ADDR` (default `http://localhost:8500`)
* `STATE_PATH` (default `/tmp/registrator-state.json`)
* `METRICS_ADDR` (default `:9090`)

---

## Declaring a service via labels

### Required label: `consul.service.<name>`

The value is HCL **containing a `service { ... }` block**.

Minimal example:

```hcl
service {
  name = "api"
  port = 8080
}
```

> ⚠️ The code enforces that `service.name` exactly matches the label suffix (`consul.service.api` ↔ `name="api"`).

### Service address (important)

If you don’t set `address`, the agent tries:

1. the Docker container **name** (without the leading `/`)
2. otherwise the `serviceName`
3. otherwise a Docker IP (first network found)

👉 In many setups, the Consul Agent (and its checks) cannot resolve container names: **set `address` explicitly** in that case.

---

## Envoy sidecar (optional)

### Enable on the agent

Environment variables:

* `SIDECAR_ENABLED=true`
* `SIDECAR_IMAGE=<image>`
* `SIDECAR_CONSUL_HTTP=<host:port or URL>` (e.g. `http://consul:8500`)
* `SIDECAR_CONSUL_GRPC=<host:port>` (e.g. `consul:8502`)
* `SIDECAR_GRPC_TLS=true|false`
* `SIDECAR_GRPC_CA_FILE=/path/to/ca.pem` (if TLS)
* `SIDECAR_PROMETHEUS_BIND_ADDR=0.0.0.0:9102` (optional; for metrics auto-check)

### Request a sidecar for a service

Add the label (value can be empty; only presence matters):

* `consul.sidecar.<name>` (e.g. `consul.sidecar.api=true`)

And in the HCL, define `connect.sidecar_service`:

```hcl
service {
  name = "api"
  port = 8080

  connect {
    sidecar_service {
      auto = true
    }
  }
}
```

When `auto = true`, the agent may:

* inject an `Envoy Ready` check on `http://<host>:19100/ready`
* inject an alias check pointing to the `serviceID`
* enable “transparent proxy” behavior (and run the sidecar with `NET_ADMIN`)

---

## Identifiers

Currently, the Consul service ID is:

* `serviceID = <full dockerContainerID> + ":" + <serviceName>`

That ID is also used to:

* find/start/clean up the sidecar (`service-id` label)
* build the `proxy-id` (`<serviceID>-sidecar-proxy`)
* inject tags into Consul

---

## State file

A simple JSON file that stores the list of known services (and hashes if used).

Default: `/tmp/registrator-state.json`

---

## Metrics

Exposed at `http://<METRICS_ADDR>/metrics`:

* `dockconsul_containers_total`
* `dockconsul_services_registered_total`
* `dockconsul_errors_total`
* `dockconsul_sidecars_launched`
* `dockconsul_sidecars_deleted`
* etc.

> Note: some metrics are defined but not fully updated by the code yet.

---

## Known limitations

* Polling every 10 seconds (no Docker event stream).
* `consul.service` (without suffix) is **not supported**.
* HCL parsing: repeated blocks of the same type may be overwritten (simplified structure).
* Default `address` strategy may not fit your network/Consul setup (often needs override).
* Limited sidecar hardening (capabilities, seccomp, etc.).
* No signal handling (clean shutdown / optional deregister on exit).

---

## TODO / Roadmap

### Correctness / Architecture

* [ ] Shorten/normalize `serviceID` (e.g., deterministic short hash) + migration plan to avoid churn.
* [ ] Configurable `address` strategy (Docker network IP, published IP, host, explicit override…).
* [ ] Auto-detect `port` from Docker (exposed/published) when missing in HCL.
* [ ] Stronger reconciliation with Consul:

  * [ ] list `/v1/agent/services` and clean only services tagged/marked `managed=true`
  * [ ] reduce reliance on local state to avoid “ghost” services

### Sidecar / Connect

* [ ] Strict “Connect-ready” validation before launching a sidecar.
* [ ] Harden the sidecar container (drop caps by default, read-only FS, seccomp/apparmor, etc.).
* [ ] Improve image compatibility (avoid assumptions like `adduser -D`, `su`, Alpine-specific behaviors).

### Observability / Ops

* [ ] Correctly update all metrics (services registered, sidecars launched/deleted per cycle…).
* [ ] Backoff + retry (avoid hammering Consul/Docker when unavailable).
* [ ] Logging: avoid dumping full payloads in production; add a debug mode.

### Ergonomics / Product

* [ ] Add Docker Compose examples + label templates.
* [ ] Document the supported HCL subset and its limitations precisely.
* [ ] Support Consul ACL token via env/flag (e.g., `CONSUL_HTTP_TOKEN`).
