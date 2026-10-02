# k6-distributed

A lab for practicing performance engineering on Kubernetes: distributed k6 load
tests, observability, and resilience
patterns under real load.

## System under test

```
k6 ──► api-gateway ──► order-service ──► inventory-service   (price + stock reservation)
                                    └──► payment-service     (used to simulate failures)
```

Request flows, error propagation, service discovery and cluster layout:
[docs/architecture.md](docs/architecture.md).

Four small Go services in `services/`. Every service exposes:

| Endpoint            | Purpose                                         |
|---------------------|-------------------------------------------------|
| `GET  /healthz`     | liveness                                        |
| `GET  /readyz`      | readiness                                       |
| `GET  /admin/fault` | show current injected fault                     |
| `POST /admin/fault` | inject faults at runtime (see below)            |
| `DELETE /admin/fault` | remove faults                                 |

Public API (via the gateway on `localhost:8080`):

- `GET  /api/products`, `GET /api/products/{id}`
- `POST /api/orders` `{"product_id":"p-1","quantity":1}`
- `GET  /api/orders/{id}`

### Fault injection

```json
{ "latency_ms": 800, "jitter_ms": 200, "error_rate": 0.3, "error_code": 503 }
```

```bash
make fault-payment FAULT='{"latency_ms":800,"error_rate":0.3}'
make heal-payment
```

## Quick start

```bash
make up      # kind cluster + build + deploy
make smoke   # k6 smoke test
make monitoring  # Prometheus + Grafana
make down    # delete the cluster
```

Worker nodes are labelled `workload=app` and `workload=loadgen` so k6 runners
never compete for CPU with the services they measure.

## Roadmap

- [x] **Phase 1**: services, fault injection, kind cluster, smoke test
- [ ] **Phase 2**: observability (kube-prometheus-stack, OTel Collector, Tempo, Grafana)
- [ ] **Phase 3**: distributed k6 with k6-operator (`TestRun`, `parallelism`), k6 metrics to Prometheus
- [ ] **Phase 4**: load-testing patterns: smoke, load, stress, spike, soak, breakpoint, plus thresholds/SLOs
- [ ] **Phase 5**: resilience: timeouts, retries + backoff/jitter, circuit breaker, bulkhead, rate limit, fallback
- [ ] **Phase 6**: service mesh (Istio ambient): move timeouts, retries, circuit breaker (outlier detection), bulkhead (connection pool) and fault injection from Go code to mesh config, then compare under the same k6 load
- [ ] **Phase 7**: chaos (Chaos Mesh) and autoscaling (HPA) under load
