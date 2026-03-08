package client

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"

	apiserver "github.com/guilhermebr/goship/internal/api"
)

// PushImage uploads a Docker image tarball to a project's VM.
func (c *Client) PushImage(idOrName, imageRef string, reader io.Reader, size int64) error {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer func() { _ = pw.Close() }()
		_ = writer.WriteField("image_ref", imageRef)
		part, err := writer.CreateFormFile("file", "image.tar.gz")
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, reader); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequest(http.MethodPost, c.baseURL+fmt.Sprintf("/api/v1/projects/%s/push-image", idOrName), pr)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req) //nolint:gosec // baseURL is user-configured
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp apiserver.ErrorResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&errResp); decErr == nil && errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("API error: %s", resp.Status)
	}

	return nil
}

// UpdateInit uploads a new goship-init binary to a project's VM.
func (c *Client) UpdateInit(idOrName string, reader io.Reader, size int64, restart bool) error {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer func() { _ = pw.Close() }()
		_ = writer.WriteField("restart", strconv.FormatBool(restart))
		part, err := writer.CreateFormFile("file", "goship-init")
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, reader); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequest(http.MethodPost, c.baseURL+fmt.Sprintf("/api/v1/projects/%s/update-init", idOrName), pr)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req) //nolint:gosec // baseURL is user-configured
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp apiserver.ErrorResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&errResp); decErr == nil && errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("API error: %s", resp.Status)
	}

	return nil
}
