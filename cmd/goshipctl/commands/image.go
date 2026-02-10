package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const (
	// Default image source is the Alpine cloud image used as a plain base.
	defaultImageURL    = "https://dl-cdn.alpinelinux.org/alpine/v3.23/releases/cloud/nocloud_alpine-3.23.3-x86_64-bios-cloudinit-r0.qcow2"
	defaultImageOutput = "~/.goship/images/goship-vm.qcow2"

	// Local build settings (--build mode).
	alpineImageURL   = defaultImageURL
	defaultImageSize = "2G"
)

// imageCmd is the parent command for image operations.
var imageCmd = &cobra.Command{
	Use:   "image",
	Short: "Manage VM base images",
	Long:  `Download and manage base VM images used by GoShip.`,
}

// imagePullCmd downloads the default VM base image.
var imagePullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Download the default GoShip VM base image",
	Long:  `Downloads the default Alpine NoCloud VM image used as GoShip base image.`,
	RunE:  runImagePull,
}

// imageBuildCmd builds a local base image from Alpine source.
var imageBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the GoShip VM image locally",
	Long:  `Builds a plain GoShip base VM image from Alpine source and resizes it.`,
	RunE:  runImageBuildCmd,
}

func init() {
	imagePullCmd.Flags().String("output", defaultImageOutput, "Output path for the downloaded image")
	imagePullCmd.Flags().Bool("force", false, "Overwrite existing image")

	imageBuildCmd.Flags().String("output", defaultImageOutput, "Output path for the built image")
	imageBuildCmd.Flags().Bool("force", false, "Overwrite existing image")
	imageBuildCmd.Flags().String("image-size", defaultImageSize, "Resize image to this size")

	imageCmd.AddCommand(imagePullCmd)
	imageCmd.AddCommand(imageBuildCmd)
}

func runImagePull(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	force, _ := cmd.Flags().GetBool("force")

	output = expandPath(output)

	// Check if file already exists.
	if _, err := os.Stat(output); err == nil {
		if !force {
			return fmt.Errorf("image already exists: %s (use --force to overwrite)", output)
		}
		// Warn about VMs backed by this image.
		if vms := checkExistingVMs(output); len(vms) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "WARNING: The following VMs have disks backed by this image and must be recreated:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", strings.Join(vms, ", "))
			fmt.Fprintf(cmd.OutOrStdout(), "  Run 'goshipctl vm destroy <name>' then 'goshipctl vm create <name>' to recreate\n\n")
		}
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	return runImageDownload(cmd.OutOrStdout(), output)
}

func runImageBuildCmd(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	force, _ := cmd.Flags().GetBool("force")
	imageSize, _ := cmd.Flags().GetString("image-size")

	output = expandPath(output)

	// Check if file already exists.
	if _, err := os.Stat(output); err == nil {
		if !force {
			return fmt.Errorf("image already exists: %s (use --force to overwrite)", output)
		}
		// Warn about VMs backed by this image.
		if vms := checkExistingVMs(output); len(vms) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "WARNING: The following VMs have disks backed by this image and must be recreated:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", strings.Join(vms, ", "))
			fmt.Fprintf(cmd.OutOrStdout(), "  Run 'goshipctl vm destroy <name>' then 'goshipctl vm create <name>' to recreate\n\n")
		}
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	return runImageBuild(cmd.OutOrStdout(), output, imageSize)
}

// runImageDownload downloads the default VM base image.
// It downloads to a temp file in the same directory, then atomically renames
// to the final output path to avoid corrupting any VM CoW disks that may
// reference the existing image.
func runImageDownload(w io.Writer, output string) error {
	tmpFile := filepath.Join(filepath.Dir(output), "."+filepath.Base(output)+".tmp")

	fmt.Fprintf(w, "Downloading GoShip VM base image...\n")
	fmt.Fprintf(w, "  URL:    %s\n", defaultImageURL)
	fmt.Fprintf(w, "  Output: %s\n\n", output)

	if err := downloadFile(w, defaultImageURL, tmpFile); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("download failed: %w", err)
	}

	info, err := os.Stat(tmpFile)
	if err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("verifying download: %w", err)
	}

	// Atomic rename from temp to final path.
	if err := os.Rename(tmpFile, output); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("finalizing image: %w", err)
	}

	fmt.Fprintf(w, "\nDownload complete: %s (%s)\n", output, formatBytes(info.Size()))
	return nil
}

// runImageBuild builds the VM image locally from an Alpine base.
// It works on a temp file in the same directory as the output, then atomically
// renames to the final path to avoid corrupting any VM CoW disks.
func runImageBuild(w io.Writer, output, imageSize string) error {
	tmpFile := filepath.Join(filepath.Dir(output), "."+filepath.Base(output)+".tmp")

	// [1/3] Preflight checks.
	fmt.Fprintf(w, "[1/3] Preflight checks...\n")

	if err := checkBuildDependencies(); err != nil {
		return err
	}

	fmt.Fprintf(w, "  qemu-img:    found\n")
	fmt.Fprintf(w, "\n")

	// [2/3] Download Alpine base image to temp file.
	fmt.Fprintf(w, "[2/3] Downloading Alpine base image...\n")
	fmt.Fprintf(w, "  URL:    %s\n", alpineImageURL)
	fmt.Fprintf(w, "  Output: %s\n\n", output)

	if err := downloadFile(w, alpineImageURL, tmpFile); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("download failed: %w", err)
	}

	// [3/3] Resize image (on temp file).
	fmt.Fprintf(w, "\n[3/3] Resizing image...\n")

	fmt.Fprintf(w, "  Resizing to %s...\n", imageSize)
	if err := resizeImage(tmpFile, imageSize); err != nil {
		os.Remove(tmpFile)
		return err
	}

	info, err := os.Stat(tmpFile)
	if err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("verifying image: %w", err)
	}

	// Atomic rename from temp to final path.
	if err := os.Rename(tmpFile, output); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("finalizing image: %w", err)
	}

	fmt.Fprintf(w, "\nBuild complete: %s (%s)\n", output, formatBytes(info.Size()))
	fmt.Fprintf(w, "  Base image ready for per-VM guest provisioning at 'vm create' time\n")
	return nil
}

// checkBuildDependencies validates that qemu-img is installed.
func checkBuildDependencies() error {
	var missing []string

	if _, err := exec.LookPath("qemu-img"); err != nil {
		missing = append(missing, "qemu-img (install: sudo apt install qemu-utils)")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required tools:\n  %s", strings.Join(missing, "\n  "))
	}
	return nil
}

// resizeImage uses qemu-img to resize the QCOW2 image.
func resizeImage(imagePath, size string) error {
	cmd := exec.Command("qemu-img", "resize", imagePath, size)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img resize failed: %w\n%s", err, string(out))
	}
	return nil
}

// checkExistingVMs scans ~/.goship/vms/*/disk.qcow2 for CoW disks backed by
// the given image path. Returns a list of VM names whose disks would be
// invalidated by replacing the base image.
func checkExistingVMs(imagePath string) []string {
	absImage, err := filepath.Abs(imagePath)
	if err != nil {
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	vmsDir := filepath.Join(home, ".goship", "vms")
	entries, err := os.ReadDir(vmsDir)
	if err != nil {
		return nil
	}

	var affected []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		diskPath := filepath.Join(vmsDir, entry.Name(), "disk.qcow2")
		if _, err := os.Stat(diskPath); err != nil {
			continue
		}

		backing := getBackingFile(diskPath)
		if backing == "" {
			continue
		}

		// Compare absolute paths.
		absBacking, err := filepath.Abs(backing)
		if err != nil {
			continue
		}
		if absBacking == absImage {
			affected = append(affected, entry.Name())
		}
	}
	return affected
}

// qemuImgInfo is the subset of qemu-img info --output=json we care about.
type qemuImgInfo struct {
	BackingFilename string `json:"backing-filename"`
}

// getBackingFile runs qemu-img info and returns the backing filename, or "".
func getBackingFile(diskPath string) string {
	out, err := exec.Command("qemu-img", "info", "--output=json", diskPath).Output()
	if err != nil {
		return ""
	}
	var info qemuImgInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return ""
	}
	return info.BackingFilename
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
