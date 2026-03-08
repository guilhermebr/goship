package apiserver

import (
	"fmt"
	"maps"
	"net/http"

	"github.com/guilhermebr/goship/internal/vault"
)

// SetEnvRequest is the request body for setting environment variables.
type SetEnvRequest struct {
	Env    map[string]string `json:"env"`
	Secret bool              `json:"secret"`
}

// DeleteEnvRequest is the request body for deleting environment variables.
type DeleteEnvRequest struct {
	Keys []string `json:"keys"`
}

// EnvResponse is the response containing environment variables.
type EnvResponse struct {
	Env map[string]string `json:"env"`
}

func (s *Server) handleEnvSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	var req SetEnvRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.Env) == 0 {
		writeError(w, http.StatusBadRequest, "env is required")
		return
	}

	if project.Env == nil {
		project.Env = make(map[string]string)
	}

	// Encrypt values if secret flag is set.
	if req.Secret {
		v, vErr := s.initVault()
		if vErr != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to initialize vault: %v", vErr))
			return
		}
		for k, val := range req.Env {
			encrypted, encErr := v.Encrypt(val)
			if encErr != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to encrypt %s: %v", k, encErr))
				return
			}
			req.Env[k] = encrypted
		}
	}

	maps.Copy(project.Env, req.Env)

	if err := s.store.UpdateProject(project); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save project: %v", err))
		return
	}

	s.logger.Printf("env set: project=%s count=%d secret=%v", project.Name, len(req.Env), req.Secret)
	writeJSON(w, http.StatusOK, EnvResponse{Env: project.Env})
}

func (s *Server) handleEnvList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	env := project.Env
	if env == nil {
		env = make(map[string]string)
	}

	// If show-values is requested, decrypt any encrypted values.
	if r.URL.Query().Get("show-values") == "true" {
		decrypted, decErr := s.decryptEnv(env)
		if decErr != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("failed to decrypt env: %v", decErr))
			return
		}
		env = decrypted
	}

	s.logger.Printf("env list: project=%s count=%d", project.Name, len(env))
	writeJSON(w, http.StatusOK, EnvResponse{Env: env})
}

func (s *Server) handleEnvDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	var req DeleteEnvRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.Keys) == 0 {
		writeError(w, http.StatusBadRequest, "keys is required")
		return
	}

	if project.Env == nil {
		writeError(w, http.StatusNotFound, "no environment variables set")
		return
	}

	for _, k := range req.Keys {
		delete(project.Env, k)
	}

	if err := s.store.UpdateProject(project); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save project: %v", err))
		return
	}

	s.logger.Printf("env delete: project=%s keys=%v", project.Name, req.Keys)
	writeJSON(w, http.StatusOK, EnvResponse{Env: project.Env})
}

// decryptEnv returns a copy of env with encrypted values decrypted.
func (s *Server) decryptEnv(env map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(env))
	var v *vault.Vault

	for k, val := range env {
		if !vault.IsEncrypted(val) {
			result[k] = val
			continue
		}
		if v == nil {
			var vErr error
			v, vErr = s.initVault()
			if vErr != nil {
				return nil, vErr
			}
		}
		plain, decErr := v.Decrypt(val)
		if decErr != nil {
			return nil, fmt.Errorf("failed to decrypt %s: %w", k, decErr)
		}
		result[k] = plain
	}

	return result, nil
}

// initVault creates a Vault instance using the master key from the data directory.
func (s *Server) initVault() (*vault.Vault, error) {
	key, err := vault.LoadOrCreateMasterKey(s.store.DataDir())
	if err != nil {
		return nil, fmt.Errorf("failed to load master key: %w", err)
	}
	return vault.New(key)
}
