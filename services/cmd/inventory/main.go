package main

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/YassineOuhadi/k6-distributed/services/internal/platform"
)

type Product struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

type store struct {
	mu       sync.Mutex
	products map[string]*Product
}

func newStore() *store {
	s := &store{products: map[string]*Product{}}
	// large stock so soak tests don't drain it
	for _, p := range []Product{
		{"p-1", "Keyboard", 49.90, 1_000_000},
		{"p-2", "Mouse", 19.90, 1_000_000},
		{"p-3", "Monitor", 199.00, 1_000_000},
		{"p-4", "Headset", 79.50, 1_000_000},
		{"p-5", "Webcam", 39.00, 1_000_000},
	} {
		s.products[p.ID] = &p
	}
	return s
}

func main() {
	s := newStore()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /products", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		out := make([]Product, 0, len(s.products))
		for _, p := range s.products {
			out = append(out, *p)
		}
		s.mu.Unlock()
		platform.WriteJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /products/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		p, ok := s.products[r.PathValue("id")]
		var cp Product
		if ok {
			cp = *p
		}
		s.mu.Unlock()
		if !ok {
			platform.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "product not found"})
			return
		}
		platform.WriteJSON(w, http.StatusOK, cp)
	})

	mux.HandleFunc("POST /reservations", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProductID string `json:"product_id"`
			Quantity  int    `json:"quantity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Quantity <= 0 {
			platform.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reservation"})
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		p, ok := s.products[req.ProductID]
		switch {
		case !ok:
			platform.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "product not found"})
		case p.Stock < req.Quantity:
			platform.WriteJSON(w, http.StatusConflict, map[string]string{"error": "insufficient stock"})
		default:
			p.Stock -= req.Quantity
			platform.WriteJSON(w, http.StatusCreated, map[string]any{"product_id": p.ID, "reserved": req.Quantity})
		}
	})

	platform.Run("inventory-service", mux)
}
