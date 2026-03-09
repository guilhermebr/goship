package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Proxy is an HTTP reverse proxy that routes requests by Host header.
type Proxy struct {
	routes *RouteTable
	// proxies caches *httputil.ReverseProxy per backend address. Entries are
	// never evicted; this is acceptable at Phase 0 scale where the number of
	// distinct backend addresses is small and bounded by running VMs.
	proxies sync.Map
	server  *http.Server
}

// New creates a new reverse proxy listening on addr.
func New(addr string, routes *RouteTable) *Proxy {
	p := &Proxy{
		routes: routes,
	}
	p.server = &http.Server{
		Addr:              addr,
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return p
}

// ServeHTTP routes incoming requests to the appropriate backend based on the Host header.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	// Strip port from Host header if present.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	backend, ok := p.routes.Lookup(host)
	if !ok {
		http.Error(w, "no backend for host", http.StatusBadGateway)
		return
	}

	rp := p.getOrCreateProxy(backend)
	if rp == nil {
		http.Error(w, "invalid backend", http.StatusBadGateway)
		return
	}

	rp.ServeHTTP(w, r) //nolint:gosec // backend from internal route table
}

// getOrCreateProxy returns a cached reverse proxy for the backend, creating one if needed.
func (p *Proxy) getOrCreateProxy(backend string) *httputil.ReverseProxy {
	if v, ok := p.proxies.Load(backend); ok {
		if rp, valid := v.(*httputil.ReverseProxy); valid {
			return rp
		}
	}

	target, err := url.Parse("http://" + backend)
	if err != nil {
		return nil
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	actual, _ := p.proxies.LoadOrStore(backend, rp)
	if result, valid := actual.(*httputil.ReverseProxy); valid {
		return result
	}
	return rp
}

// ListenAndServe starts the proxy server.
func (p *Proxy) ListenAndServe() error {
	return p.server.ListenAndServe()
}

// Close shuts down the proxy server.
func (p *Proxy) Close() error {
	return p.server.Close()
}
