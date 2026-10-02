# Telemetry

```
shop services ──OTLP/HTTP :4318──► otel-collector ──OTLP──► Prometheus  (metrics)
  (OTel Go SDK)                    (monitoring ns)  └─OTLP─► Tempo       (traces)
                                                               │
                                                               └─ service graph metrics ─► Prometheus

Grafana :3000 ── datasources: Prometheus, Tempo
```
Each service uses the OpenTelemetry Go SDK (`platform/telemetry.go`):

- `otelhttp.NewHandler` on the server: one span and one duration sample per request
- `otelhttp.NewTransport` on outgoing calls (order client, gateway proxy): client span,
  and the `traceparent` header so the trace continues in the next service
- server spans and metrics carry `http.route` (`/orders/{id}`, not the raw path)
- `/healthz`, `/readyz`, `/admin/*` are not traced
- access logs include `trace_id`

Configured with standard env vars (`deploy/k8s/shop.yaml`). No endpoint set means telemetry off.

| Env | Value |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://otel-collector.monitoring:4318` |
| `OTEL_RESOURCE_ATTRIBUTES` | `service.instance.id=<pod>,k8s.pod.name=<pod>,k8s.namespace.name=shop` |

## Metrics in Prometheus

Prometheus receives OTLP directly (`enableOTLPReceiver`). `service.name` becomes the
`job` label, `service.instance.id` becomes `instance`.

| Metric | Labels |
|---|---|
| `http_server_request_duration_seconds_{bucket,count,sum}` | `job`, `instance`, `http_route`, `http_request_method`, `http_response_status_code` |
| `http_client_request_duration_seconds_{bucket,count,sum}` | `job`, `server_address`, `http_request_method`, `http_response_status_code` |

Examples:

```promql
# requests/s per service
sum by (job) (rate(http_server_request_duration_seconds_count[1m]))

# error ratio per service
sum by (job) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[1m]))
  / sum by (job) (rate(http_server_request_duration_seconds_count[1m]))

# p95 latency per service and route
histogram_quantile(0.95, sum by (job, http_route, le) (rate(http_server_request_duration_seconds_bucket[1m])))

# calls between services
sum by (job, server_address) (rate(http_client_request_duration_seconds_count[1m]))
```

## Traces in Tempo

Tempo runs as a single binary (`deploy/monitoring/tempo.yaml`), keeps 24h of traces,
and receives OTLP from otel-collector on `tempo.monitoring:4317`.

One `POST /api/orders` gives one trace with 9 spans:

```
api-gateway        SERVER  POST /api/orders
api-gateway        CLIENT  HTTP POST
order-service      SERVER  POST /orders
order-service      CLIENT  HTTP GET
inventory-service  SERVER  GET /products/{id}
order-service      CLIENT  HTTP POST
inventory-service  SERVER  POST /reservations
order-service      CLIENT  HTTP POST
payment-service    SERVER  POST /payments
```

A 5xx marks the span as error, so a payment failure shows as error on payment,
order and gateway spans.

In Grafana: **Explore → Tempo**. TraceQL examples:

```
# failed orders
{ resource.service.name = "api-gateway" && span.http.route = "/api/orders" && status = error }

# slow payment calls
{ resource.service.name = "payment-service" && duration > 500ms }

# one trace from a log line
<trace_id from the access log>
```

## Service graph

Tempo's metrics generator (`service-graphs` processor) builds caller → callee metrics
from the spans and writes them to Prometheus (remote write).

| Metric | Labels |
|---|---|
| `traces_service_graph_request_total` | `client`, `server` |
| `traces_service_graph_request_failed_total` | `client`, `server` |
| `traces_service_graph_request_server_seconds_{bucket,count,sum}` | `client`, `server` |

Edges found from real traffic:

```
user -> api-gateway
api-gateway -> inventory-service
api-gateway -> order-service
order-service -> inventory-service
order-service -> payment-service
```

## Shop dashboard

`deploy/monitoring/dashboards/shop.json`, loaded by `make dashboards` as a ConfigMap
labelled `grafana_dashboard=1`.

| Row | Panels |
|---|---|
| Overview (api-gateway) | requests/s, error rate, p95, p99 |
| Per service (RED) | requests/s, error rate, p95 per service |
| Routes and status codes | p95 per route, responses per status code |
| Dependencies | service map (Tempo), p95 of outgoing calls per caller → callee |
| Traces | recent error traces, traces slower than 500ms |
| Pods (namespace shop) | CPU, memory |