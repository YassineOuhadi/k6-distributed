package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/YassineOuhadi/k6-distributed/services/internal/platform"
)

func main() {
	processing, _ := strconv.Atoi(platform.Env("PROCESSING_MS", "20"))
	mux := http.NewServeMux()

	mux.HandleFunc("POST /payments", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrderID string  `json:"order_id"`
			Amount  float64 `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 {
			platform.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payment"})
			return
		}
		select {
		case <-time.After(time.Duration(processing) * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		platform.WriteJSON(w, http.StatusCreated, map[string]any{
			"payment_id": "pay-" + rand.Text()[:12],
			"order_id":   req.OrderID,
			"amount":     req.Amount,
			"status":     "approved",
		})
	})

	platform.Run("payment-service", mux)
}
