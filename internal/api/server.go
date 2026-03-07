package apiserver

import (
	"log"
	"net/http"
	"time"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	"github.com/guilhermebr/goship/internal/shared/state"
)

// Server is the GoShip REST API server.
type Server struct {
	store  *state.Store
	rt     runtime.ProjectRuntime
	mux    *http.ServeMux
	logger *log.Logger
}

// New creates a new API server and registers all routes.
func New(store *state.Store, rt runtime.ProjectRuntime, logger *log.Logger) *Server {
	s := &Server{
		store:  store,
		rt:     rt,
		mux:    http.NewServeMux(),
		logger: logger,
	}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// routes registers all API routes.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.logMiddleware(s.handleHealthz))

	// Projects
	s.mux.HandleFunc("GET /api/v1/projects", s.logMiddleware(s.handleProjectList))
	s.mux.HandleFunc("POST /api/v1/projects", s.logMiddleware(s.handleProjectCreate))
	s.mux.HandleFunc("GET /api/v1/projects/{id}", s.logMiddleware(s.handleProjectGet))
	s.mux.HandleFunc("DELETE /api/v1/projects/{id}", s.logMiddleware(s.handleProjectDelete))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/stop", s.logMiddleware(s.handleProjectStop))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/start", s.logMiddleware(s.handleProjectStart))

	// Apps
	s.mux.HandleFunc("GET /api/v1/projects/{id}/apps", s.logMiddleware(s.handleAppList))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/apps", s.logMiddleware(s.handleAppCreate))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/apps/{name}", s.logMiddleware(s.handleAppGet))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/apps/{name}/deploy", s.logMiddleware(s.handleAppDeploy))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/apps/{name}/stop", s.logMiddleware(s.handleAppStop))
	s.mux.HandleFunc("DELETE /api/v1/projects/{id}/apps/{name}", s.logMiddleware(s.handleAppDelete))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/apps/{name}/logs", s.logMiddleware(s.handleAppLogs))
}

// handleHealthz returns a simple health check response.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// requireRuntime checks that the runtime is available and writes 503 if not.
func (s *Server) requireRuntime(w http.ResponseWriter) bool {
	if s.rt == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime not available")
		return false
	}
	return true
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter

	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// logMiddleware wraps a handler with request logging.
func (s *Server) logMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path = path + "?" + r.URL.RawQuery
		}
		s.logger.Printf("%s %s %s %d %s", r.RemoteAddr, r.Method, path, rec.status, time.Since(start))
	}
}
