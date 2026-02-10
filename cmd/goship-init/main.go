package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	gsinit "github.com/guilhermebr/goship/internal/init"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Printf("goship-init %s (commit: %s, built: %s)", Version, Commit, BuildTime)
	log.Printf("PID: %d", os.Getpid())

	if os.Getpid() == 1 {
		performInitDuties()
	}

	device := os.Getenv("GOSHIP_SERIAL_DEVICE")
	if device == "" {
		device = gsinit.DefaultSerialDevice
	}

	agent, err := gsinit.New(device)
	if err != nil {
		log.Fatalf("goship-init: failed to create agent: %v", err)
	}

	if err := agent.Run(); err != nil {
		log.Fatalf("goship-init: agent exited with error: %v", err)
	}

	log.Println("goship-init: shutdown complete")
}

// performInitDuties handles PID 1 responsibilities: mounting filesystems,
// setting hostname, and configuring basic networking.
func performInitDuties() {
	log.Println("goship-init: running as PID 1, performing init duties")

	// Mount essential filesystems.
	mounts := []struct {
		source string
		target string
		fstype string
		flags  uintptr
	}{
		{"proc", "/proc", "proc", 0},
		{"sysfs", "/sys", "sysfs", 0},
		{"devtmpfs", "/dev", "devtmpfs", 0},
		{"devpts", "/dev/pts", "devpts", 0},
		{"tmpfs", "/run", "tmpfs", 0},
		{"cgroup2", "/sys/fs/cgroup", "cgroup2", 0},
	}

	for _, m := range mounts {
		if isMounted(m.target) {
			continue
		}
		if err := os.MkdirAll(m.target, 0755); err != nil {
			log.Printf("warn: failed to create mount point %s: %v", m.target, err)
			continue
		}
		if err := syscall.Mount(m.source, m.target, m.fstype, m.flags, ""); err != nil {
			log.Printf("warn: failed to mount %s on %s: %v", m.fstype, m.target, err)
		}
	}

	// Set hostname.
	hostname := os.Getenv("GOSHIP_HOSTNAME")
	if hostname == "" {
		hostname = "goship-vm"
	}
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		log.Printf("warn: failed to set hostname: %v", err)
	}

	// Bring up loopback interface.
	if err := runCmd("ip", "link", "set", "lo", "up"); err != nil {
		log.Printf("warn: failed to bring up lo: %v", err)
	}

	// Bring up eth0.
	if err := runCmd("ip", "link", "set", "eth0", "up"); err != nil {
		log.Printf("warn: failed to bring up eth0: %v", err)
	}

	// DHCP on eth0 — try udhcpc first (Alpine), fall back to dhclient.
	if err := runCmd("udhcpc", "-i", "eth0", "-q"); err != nil {
		log.Printf("warn: udhcpc failed: %v, trying dhclient", err)
		if err := runCmd("dhclient", "eth0"); err != nil {
			log.Printf("warn: dhclient also failed: %v", err)
		}
	}

	log.Println("goship-init: init duties complete")
}

// isMounted checks whether the given path is already a mount point by reading /proc/mounts.
func isMounted(path string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == path {
			return true
		}
	}
	return false
}

// runCmd runs an external command and returns any error.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
