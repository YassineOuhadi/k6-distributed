package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/YassineOuhadi/k6-distributed/services/internal/platform"
)

func proxy(target string) http.Handler {
	u, err := url.Parse(target)
	if err != nil {
		panic(err)
	}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, "/api")
			r.Out.URL.RawPath = ""
			r.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			platform.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		},
	}
}

func main() {
	inventory := proxy(platform.Env("INVENTORY_URL", "http://localhost:8083"))
	order := proxy(platform.Env("ORDER_URL", "http://localhost:8081"))

	mux := http.NewServeMux()
	mux.Handle("/api/products", inventory)
	mux.Handle("/api/products/", inventory)
	mux.Handle("/api/orders", order)
	mux.Handle("/api/orders/", order)

	platform.Run("api-gateway", mux)
}
