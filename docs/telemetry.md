# Telemetry

```
shop services ──OTLP/HTTP :4318──► otel-collector ──OTLP──► Prometheus   (metrics)
  (OTel Go SDK)                    (monitoring ns)  └──────► debug log   (traces, Tempo)
                                                                │
                                                             Grafana :3000
```

Each service uses the OpenTelemetry Go SDK (`platform/telemetry.go`):

- `otelhttp.NewHandler` on the server: one span and one duration sample per request
- `otelhttp.NewTransport` on outgoing calls (order client, gateway proxy): client span,
  and the `traceparent` header so the trace continues in the next service
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
