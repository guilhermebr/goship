package apiserver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const maxUploadSize = 2 << 30 // 2 GB

func (s *Server) handlePushImage(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	instance := s.store.GetInstance(project.ID)
	if instance == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no VM instance for project %s", id))
		return
	}

	if parseErr := r.ParseMultipartForm(maxUploadSize); parseErr != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse multipart form: %v", parseErr))
		return
	}

	imageRef := r.FormValue("image_ref")
	if imageRef == "" {
		writeError(w, http.StatusBadRequest, "image_ref is required")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("file is required: %v", err))
		return
	}
	defer func() { _ = file.Close() }()

	// Compute checksum while reading.
	h := sha256.New()
	tee := io.TeeReader(file, h)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	s.rt.LoadInstance(instance)

	if uploadErr := s.rt.UploadImage(ctx, instance.ID, imageRef, tee, header.Size, ""); uploadErr != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to upload image: %v", uploadErr))
		return
	}

	checksum := fmt.Sprintf("%x", h.Sum(nil))
	s.logger.Printf("image pushed: project=%s image=%s size=%d sha256=%s",
		project.Name, imageRef, header.Size, checksum)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "image": imageRef, "sha256": checksum})
}

//nolint:funlen // Handler with upload + optional restart sequence
func (s *Server) handleUpdateInit(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	instance := s.store.GetInstance(project.ID)
	if instance == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no VM instance for project %s", id))
		return
	}

	if parseErr := r.ParseMultipartForm(maxUploadSize); parseErr != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse multipart form: %v", parseErr))
		return
	}

	restartStr := r.FormValue("restart")
	restart, _ := strconv.ParseBool(restartStr)

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("file is required: %v", err))
		return
	}
	defer func() { _ = file.Close() }()

	// Compute checksum.
	h := sha256.New()
	tee := io.TeeReader(file, h)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	s.rt.LoadInstance(instance)

	// Upload the binary as a goship-init update.
	if err := s.rt.UploadBinary(ctx, instance.ID, "goship-init", "goship-init", tee, header.Size, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to upload init binary: %v", err))
		return
	}

	checksum := fmt.Sprintf("%x", h.Sum(nil))
	s.logger.Printf("init updated: project=%s size=%d sha256=%s", project.Name, header.Size, checksum)

	// Optionally restart.
	if restart {
		if err := s.rt.StopInstance(ctx, instance.ID); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to stop VM for restart: %v", err))
			return
		}

		// Wait for stop.
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				writeError(w, http.StatusGatewayTimeout, "timed out waiting for VM to stop after init update")
				return
			case <-ticker.C:
				status, err := s.rt.GetInstanceStatus(ctx, instance.ID)
				if err != nil {
					continue
				}
				if status.State == "stopped" {
					goto startVM
				}
			}
		}
	startVM:
		if err := s.rt.StartInstance(ctx, instance.ID); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start VM after init update: %v", err))
			return
		}
		s.logger.Printf("init updated and VM restarted: project=%s", project.Name)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "sha256": checksum})
}
