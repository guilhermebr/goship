package commands

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const (
	// Alpine Linux nocloud image — BIOS-compatible, cloud-init included.
	alpineImageURL = "https://dl-cdn.alpinelinux.org/alpine/v3.23/releases/cloud/nocloud_alpine-3.23.3-x86_64-bios-cloudinit-r0.qcow2"

	defaultImageOutput = "~/.goship/images/goship-vm.qcow2"
)

// imageCmd is the parent command for image operations.
var imageCmd = &cobra.Command{
	Use:   "image",
	Short: "Manage VM base images",
	Long:  `Download and manage base VM images used by GoShip.`,
}

// imagePullCmd downloads the Alpine base image.
var imagePullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Download the Alpine base VM image",
	Long:  `Downloads the Alpine Linux nocloud QCOW2 image for use as the base VM image.`,
	RunE:  runImagePull,
}

func init() {
	imagePullCmd.Flags().String("output", defaultImageOutput, "Output path for the downloaded image")
	imagePullCmd.Flags().Bool("force", false, "Overwrite existing image")

	imageCmd.AddCommand(imagePullCmd)
}

func runImagePull(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	force, _ := cmd.Flags().GetBool("force")

	output = expandPath(output)

	// Check if file already exists.
	if _, err := os.Stat(output); err == nil && !force {
		return fmt.Errorf("image already exists: %s (use --force to overwrite)", output)
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Downloading Alpine base image...\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  URL:    %s\n", alpineImageURL)
	fmt.Fprintf(cmd.OutOrStdout(), "  Output: %s\n\n", output)

	if err := downloadFile(cmd.OutOrStdout(), alpineImageURL, output); err != nil {
		// Clean up partial download.
		os.Remove(output)
		return fmt.Errorf("download failed: %w", err)
	}

	// Show final file size.
	info, err := os.Stat(output)
	if err != nil {
		return fmt.Errorf("verifying download: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nDownload complete: %s (%s)\n", output, formatBytes(info.Size()))
	return nil
}

// downloadFile fetches a URL and writes it to dst, showing progress on w.
func downloadFile(w io.Writer, url, dst string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	pw := &progressWriter{
		w:     w,
		total: resp.ContentLength,
	}
	pw.startTicker()
	defer pw.stop()

	if _, err := io.Copy(f, io.TeeReader(resp.Body, pw)); err != nil {
		return err
	}

	// Print final progress line.
	pw.printProgress()
	return nil
}

// progressWriter tracks bytes written and prints progress to a terminal.
type progressWriter struct {
	w       io.Writer
	total   int64
	written int64
	mu      sync.Mutex
	ticker  *time.Ticker
	done    chan struct{}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.mu.Lock()
	pw.written += int64(n)
	pw.mu.Unlock()
	return n, nil
}

func (pw *progressWriter) startTicker() {
	pw.ticker = time.NewTicker(500 * time.Millisecond)
	pw.done = make(chan struct{})
	go func() {
		for {
			select {
			case <-pw.ticker.C:
				pw.printProgress()
			case <-pw.done:
				return
			}
		}
	}()
}

func (pw *progressWriter) stop() {
	pw.ticker.Stop()
	close(pw.done)
}

func (pw *progressWriter) printProgress() {
	pw.mu.Lock()
	written := pw.written
	total := pw.total
	pw.mu.Unlock()

	if total > 0 {
		pct := float64(written) / float64(total) * 100
		fmt.Fprintf(pw.w, "\r  Progress: %s / %s (%.0f%%)", formatBytes(written), formatBytes(total), pct)
	} else {
		fmt.Fprintf(pw.w, "\r  Progress: %s", formatBytes(written))
	}
}

// formatBytes returns a human-readable byte string.
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
