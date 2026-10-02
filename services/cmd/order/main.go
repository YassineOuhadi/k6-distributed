package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/YassineOuhadi/k6-distributed/services/internal/platform"
)

type Order struct {
	ID        string    `json:"id"`
	ProductID string    `json:"product_id"`
	Quantity  int       `json:"quantity"`
	Total     float64   `json:"total"`
	PaymentID string    `json:"payment_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func main() {
	timeoutMs, err := strconv.Atoi(platform.Env("UPSTREAM_TIMEOUT_MS", "30000"))
	if err != nil {
		panic("invalid UPSTREAM_TIMEOUT_MS: " + err.Error())
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	inventory := platform.NewClient(platform.Env("INVENTORY_URL", "http://localhost:8083"), timeout)
	payment := platform.NewClient(platform.Env("PAYMENT_URL", "http://localhost:8082"), timeout)

	var mu sync.RWMutex
	orders := map[string]Order{}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProductID string `json:"product_id"`
			Quantity  int    `json:"quantity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProductID == "" || req.Quantity <= 0 {
			platform.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order"})
			return
		}
		ctx := r.Context()

		var product struct {
			Price float64 `json:"price"`
		}
		if err := inventory.Do(ctx, http.MethodGet, "/products/"+req.ProductID, nil, &product); err != nil {
			writeUpstreamError(w, err)
			return
		}
		if err := inventory.Do(ctx, http.MethodPost, "/reservations", req, nil); err != nil {
			writeUpstreamError(w, err)
			return
		}

		o := Order{
			ID:        "ord-" + rand.Text()[:12],
			ProductID: req.ProductID,
			Quantity:  req.Quantity,
			Total:     product.Price * float64(req.Quantity),
			CreatedAt: time.Now().UTC(),
		}
		var pay struct {
			PaymentID string `json:"payment_id"`
		}
		if err := payment.Do(ctx, http.MethodPost, "/payments", map[string]any{"order_id": o.ID, "amount": o.Total}, &pay); err != nil {
			writeUpstreamError(w, err)
			return
		}
		o.PaymentID, o.Status = pay.PaymentID, "confirmed"

		mu.Lock()
		orders[o.ID] = o
		mu.Unlock()
		platform.WriteJSON(w, http.StatusCreated, o)
	})

	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		o, ok := orders[r.PathValue("id")]
		mu.RUnlock()
		if !ok {
			platform.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "order not found"})
			return
		}
		platform.WriteJSON(w, http.StatusOK, o)
	})

	platform.Run("order-service", mux)
}

// 4xx passes through, timeout -> 504, anything else -> 502.
func writeUpstreamError(w http.ResponseWriter, err error) {
	slog.Error("upstream call failed", "err", err)
	var ue *platform.UpstreamError
	switch {
	case errors.As(err, &ue) && ue.Status < 500:
		platform.WriteJSON(w, ue.Status, map[string]string{"error": err.Error()})
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		platform.WriteJSON(w, http.StatusGatewayTimeout, map[string]string{"error": err.Error()})
	default:
		platform.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}
