package apiserver

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// RegisterNodeRequest is the request body for registering a node.
type RegisterNodeRequest struct {
	Hostname  string             `json:"hostname"`
	Endpoint  string             `json:"endpoint"`
	Resources entities.Resources `json:"resources"`
	Labels    map[string]string  `json:"labels,omitempty"`
}

func (s *Server) handleNodeRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterNodeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Hostname == "" {
		writeError(w, http.StatusBadRequest, "hostname is required")
		return
	}

	// Check for duplicate hostname.
	existing := s.store.ListNodes()
	for _, n := range existing {
		if n.Hostname == req.Hostname {
			writeError(w, http.StatusConflict, fmt.Sprintf("node already exists: %s", req.Hostname))
			return
		}
	}

	now := time.Now()
	node := &entities.Node{
		ID:            uuid.New().String(),
		Hostname:      req.Hostname,
		Endpoint:      req.Endpoint,
		Status:        entities.NodeStatusOnline,
		Resources:     req.Resources,
		Labels:        req.Labels,
		LastHeartbeat: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	if err := s.store.SetNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to register node: %v", err))
		return
	}

	s.logger.Printf("node registered: %s (%s)", node.Hostname, node.ID)
	writeJSON(w, http.StatusCreated, node)
}

func (s *Server) handleNodeList(w http.ResponseWriter, _ *http.Request) {
	nodes := s.store.ListNodes()
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) handleNodeGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := s.store.GetNode(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleNodeDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteNode(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logger.Printf("node deleted: %s", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNodeDrain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := s.store.GetNode(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	node.Status = entities.NodeStatusDraining
	node.UpdatedAt = time.Now()

	if err := s.store.SetNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to drain node: %v", err))
		return
	}

	s.logger.Printf("node draining: %s (%s)", node.Hostname, node.ID)
	writeJSON(w, http.StatusOK, node)
}
