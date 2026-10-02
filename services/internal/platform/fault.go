package platform

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

type FaultConfig struct {
	LatencyMs int     `json:"latency_ms"`
	JitterMs  int     `json:"jitter_ms"`
	ErrorRate float64 `json:"error_rate"` // 0..1
	ErrorCode int     `json:"error_code"` // default 503
}

type FaultInjector struct {
	mu  sync.RWMutex
	cfg FaultConfig
}

func (f *FaultInjector) Get() FaultConfig {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cfg
}

func (f *FaultInjector) Set(c FaultConfig) {
	if c.ErrorCode == 0 {
		c.ErrorCode = http.StatusServiceUnavailable
	}
	c.ErrorRate = min(max(c.ErrorRate, 0), 1)
	f.mu.Lock()
	f.cfg = c
	f.mu.Unlock()
}

func (f *FaultInjector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		c := f.Get()
		delay := time.Duration(c.LatencyMs) * time.Millisecond
		if c.JitterMs > 0 {
			delay += time.Duration(rand.IntN(c.JitterMs)) * time.Millisecond
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if c.ErrorRate > 0 && rand.Float64() < c.ErrorRate {
			WriteJSON(w, c.ErrorCode, map[string]string{"error": "injected fault"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (f *FaultInjector) AdminHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost, http.MethodPut:
		var c FaultConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		f.Set(c)
	case http.MethodDelete:
		f.Set(FaultConfig{})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	WriteJSON(w, http.StatusOK, f.Get())
}
