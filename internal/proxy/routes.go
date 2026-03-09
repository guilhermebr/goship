package proxy

import (
	"cmp"
	"slices"
	"sync"
)

// Route represents a mapping from a domain to a backend address.
type Route struct {
	Domain  string `json:"domain"`
	Backend string `json:"backend"`
}

// RouteTable is a thread-safe mapping of domain names to backend addresses.
type RouteTable struct {
	mu     sync.RWMutex
	routes map[string]string
}

// NewRouteTable creates a new empty route table.
func NewRouteTable() *RouteTable {
	return &RouteTable{
		routes: make(map[string]string),
	}
}

// Set adds or updates a route mapping.
func (rt *RouteTable) Set(domain, backend string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.routes[domain] = backend
}

// Delete removes a route mapping.
func (rt *RouteTable) Delete(domain string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.routes, domain)
}

// Lookup returns the backend for a domain and whether it was found.
func (rt *RouteTable) Lookup(domain string) (string, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	backend, ok := rt.routes[domain]
	return backend, ok
}

// All returns all routes as a slice, sorted by domain for deterministic output.
func (rt *RouteTable) All() []Route {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	routes := make([]Route, 0, len(rt.routes))
	for domain, backend := range rt.routes {
		routes = append(routes, Route{Domain: domain, Backend: backend})
	}
	slices.SortFunc(routes, func(a, b Route) int {
		return cmp.Compare(a.Domain, b.Domain)
	})
	return routes
}
