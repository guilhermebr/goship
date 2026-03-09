package proxy_test

import (
	"sync"
	"testing"

	"github.com/guilhermebr/goship/internal/proxy"
)

func TestRouteTable_SetAndLookup(t *testing.T) {
	rt := proxy.NewRouteTable()

	rt.Set("web.example.com", "192.168.1.10:8080")

	backend, ok := rt.Lookup("web.example.com")
	if !ok {
		t.Fatal("expected route to be found")
	}
	if backend != "192.168.1.10:8080" {
		t.Fatalf("expected backend '192.168.1.10:8080', got %s", backend)
	}
}

func TestRouteTable_LookupMissing(t *testing.T) {
	rt := proxy.NewRouteTable()

	_, ok := rt.Lookup("nonexistent.example.com")
	if ok {
		t.Fatal("expected route to not be found")
	}
}

func TestRouteTable_Delete(t *testing.T) {
	rt := proxy.NewRouteTable()

	rt.Set("web.example.com", "192.168.1.10:8080")
	rt.Delete("web.example.com")

	_, ok := rt.Lookup("web.example.com")
	if ok {
		t.Fatal("expected route to be deleted")
	}
}

func TestRouteTable_Update(t *testing.T) {
	rt := proxy.NewRouteTable()

	rt.Set("web.example.com", "192.168.1.10:8080")
	rt.Set("web.example.com", "192.168.1.20:9090")

	backend, ok := rt.Lookup("web.example.com")
	if !ok {
		t.Fatal("expected route to be found")
	}
	if backend != "192.168.1.20:9090" {
		t.Fatalf("expected updated backend, got %s", backend)
	}
}

func TestRouteTable_All(t *testing.T) {
	rt := proxy.NewRouteTable()

	rt.Set("web.example.com", "192.168.1.10:8080")
	rt.Set("api.example.com", "192.168.1.10:3000")

	routes := rt.All()
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	// All() returns sorted by domain, so no manual sort needed.
	if routes[0].Domain != "api.example.com" {
		t.Fatalf("expected api.example.com, got %s", routes[0].Domain)
	}
	if routes[1].Domain != "web.example.com" {
		t.Fatalf("expected web.example.com, got %s", routes[1].Domain)
	}
}

func TestRouteTable_ConcurrentAccess(t *testing.T) {
	rt := proxy.NewRouteTable()

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			domain := "app.example.com"
			rt.Set(domain, "192.168.1.10:8080")
			rt.Lookup(domain)
			rt.All()
			rt.Delete(domain)
		})
	}
	wg.Wait()
}
