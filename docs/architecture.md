# Architecture

## Overview

```
                     ┌──────────────────── kind cluster "k6-lab" ─────────────────────┐
                     │                                                                │
                     │  node: worker (workload=app)            namespace: shop        │
                     │                                                                │
 k6 ── :8080 ──► NodePort 30080 ──► api-gateway (x2)                                  │
                     │                    │                                           │
                     │                    ├── /api/products* ──► inventory-service    │
                     │                    │                                           │
                     │                    └── /api/orders* ────► order-service        │
                     │                                              │                 │
                     │                                              ├──► inventory-service
                     │                                              └──► payment-service
                     │                                                                │
                     │  node: worker2 (workload=loadgen)   k6 runners.                │
                     └────────────────────────────────────────────────────────────────┘
```

## Services

| Service | Replicas | Role | Calls |
|---|---|---|---|
| api-gateway | 2 | Reverse proxy, strips `/api` prefix. No business logic. | inventory, order |
| order-service | 1 | Orchestrates order creation. Stores orders in memory. | inventory, payment |
| inventory-service | 1 | Product catalog and stock. Stores stock in memory. | — |
| payment-service | 1 | Simulated payment provider (20 ms processing, `PROCESSING_MS`). | — |

## API

### Public (through the gateway)

| Method | Path | Routed to |
|---|---|---|
| GET | `/api/products` | inventory `GET /products` |
| GET | `/api/products/{id}` | inventory `GET /products/{id}` |
| POST | `/api/orders` | order `POST /orders` |
| GET | `/api/orders/{id}` | order `GET /orders/{id}` |

### Internal

| Service | Method | Path | Body | Responses |
|---|---|---|---|---|
| inventory | GET | `/products/{id}` | — | 200, 404 |
| inventory | POST | `/reservations` | `{"product_id","quantity"}` | 201, 400, 404, 409 (no stock) |
| payment | POST | `/payments` | `{"order_id","amount"}` | 201, 400 |

### Every service

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | liveness probe |
| GET | `/readyz` | readiness probe |
| GET/POST/DELETE | `/admin/fault` | read / set / clear injected faults |

## Request flows

### Create order: `POST /api/orders`

One client request produces 4 internal calls, executed sequentially.

```
k6            gateway         order           inventory        payment
│ POST /api/orders              │                 │                │
│────────────►│ POST /orders    │                 │                │
│             │────────────────►│ GET /products/{id}               │
│             │                 │────────────────►│ price          │
│             │                 │◄────────────────│                │
│             │                 │ POST /reservations               │
│             │                 │────────────────►│ stock -= qty   │
│             │                 │◄────────────────│                │
│             │                 │ POST /payments                   │
│             │                 │─────────────────────────────────►│
│             │                 │◄─────────────────────────────────│ approved
│             │                 │ save order                       │
│◄────────────│◄────────────────│ 201                              │
```

Latency of the order = gateway + order + inventory x2 + payment. Any slow
dependency adds directly to the total.

### Get order: `GET /api/orders/{id}`

gateway → order. Read from memory, no downstream call.

### List products: `GET /api/products`

gateway → inventory. No call to order or payment.

## Error propagation

`order-service` maps downstream errors (`writeUpstreamError`):

| Downstream result | order-service returns |
|---|---|
| 4xx (e.g. 404 product, 409 stock) | same 4xx |
| timeout | 504 |
| 5xx or connection error | 502 |

The gateway forwards the status as-is, or returns 502 if order/inventory is unreachable.

Example with payment returning 503:

```
payment 503 → order 502 → gateway 502 → k6 sees 502 on POST /api/orders
```

Logs: 5xx responses are logged at `ERROR`, 4xx at `WARN`. order-service also
logs `upstream call failed` with the downstream URL and status.

## Fault injection

Every service wraps its handlers with a fault middleware (`platform/fault.go`).
Config is per pod and held in memory:

```json
{ "latency_ms": 800, "jitter_ms": 200, "error_rate": 0.3, "error_code": 503 }
```

- `latency_ms` + random `[0, jitter_ms)` added before the handler runs
- `error_rate` probability of returning `error_code` instead of calling the handler
- `/healthz`, `/readyz` and `/admin/*` are never affected

Applied through `make fault-payment FAULT='...'` and cleared with `make heal-payment`.
Since the config is per pod, a service with several replicas needs it set on each pod.

## Service discovery

| Service | Env | Value |
|---|---|---|
| api-gateway | `INVENTORY_URL` | `http://inventory-service:8080` |
| api-gateway | `ORDER_URL` | `http://order-service:8080` |
| order-service | `INVENTORY_URL` | `http://inventory-service:8080` |
| order-service | `PAYMENT_URL` | `http://payment-service:8080` |
| order-service | `UPSTREAM_TIMEOUT_MS` | `2000` |

Each name is a ClusterIP Service resolved by cluster DNS and load-balanced
across the pods of that Deployment.

## Cluster layout

| Node | Label | Runs |
|---|---|---|
| control-plane | — | Kubernetes control plane |
| worker | `workload=app` | shop services (`nodeSelector`) |
| worker2 | `workload=loadgen` | k6 runners (phase 3) |

Resources per pod: request 100m CPU / 32Mi, limit 500m CPU / 128Mi.
