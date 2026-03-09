package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guilhermebr/goship/internal/proxy"
)

func TestProxy_RoutesToBackend(t *testing.T) {
	// Create a backend server.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	// Strip "http://" from backend URL to get host:port.
	backendAddr := backend.Listener.Addr().String()

	routes := proxy.NewRouteTable()
	routes.Set("web.example.com", backendAddr)

	p := proxy.New(":0", routes)

	// Test via ServeHTTP directly.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "web.example.com"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	if string(body) != "hello from backend" {
		t.Fatalf("expected 'hello from backend', got %s", string(body))
	}
}

func TestProxy_RoutesToBackendWithPort(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	backendAddr := backend.Listener.Addr().String()

	routes := proxy.NewRouteTable()
	routes.Set("web.example.com", backendAddr)

	p := proxy.New(":0", routes)

	// Host header with port should still match.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "web.example.com:8081"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestProxy_UnknownHost(t *testing.T) {
	routes := proxy.NewRouteTable()
	p := proxy.New(":0", routes)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "unknown.example.com"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}
