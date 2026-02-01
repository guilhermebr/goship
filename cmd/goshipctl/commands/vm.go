package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	libvirtRuntime "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
)

// vmCmd is the parent command for VM lifecycle operations.
var vmCmd = &cobra.Command{
	Use:   "vm",
	Short: "VM lifecycle commands (experimental)",
	Long:  `Manage VM lifecycle: create, destroy, and list VMs via libvirt. Experimental commands for learning VM lifecycle.`,
}

// vmCreateCmd creates a new VM.
var vmCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create and start a VM",
	Long:  `Creates a CoW disk image, generates domain XML, defines and starts a VM via libvirt.`,
	RunE:  runVMCreate,
}

// vmDestroyCmd destroys an existing VM.
var vmDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy a VM",
	Long:  `Stops a running VM, undefines it from libvirt, and optionally removes its disk image.`,
	RunE:  runVMDestroy,
}

// vmListCmd lists GoShip-managed VMs.
var vmListCmd = &cobra.Command{
	Use:   "list",
	Short: "List GoShip VMs",
	Long:  `Lists all libvirt domains with the "goship-" prefix and their current state.`,
	RunE:  runVMList,
}

func init() {
	// vm create flags
	vmCreateCmd.Flags().String("name", "", "VM name (required)")
	vmCreateCmd.Flags().String("base-image", "~/.goship/images/goship-vm.qcow2", "Base qcow2 image path")
	vmCreateCmd.Flags().Int64("memory", 512, "Memory in MB")
	vmCreateCmd.Flags().Int("cpus", 1, "Number of CPU cores")
	vmCreateCmd.Flags().Bool("enable-kvm", true, "Enable KVM acceleration")
	vmCreateCmd.Flags().String("network-type", "network", "Network type (network, bridge, user)")
	vmCreateCmd.Flags().String("network-source", "default", "Network source name")
	vmCreateCmd.Flags().String("data-dir", "~/.goship", "Data directory for VM disk images")
	_ = vmCreateCmd.MarkFlagRequired("name")

	// vm destroy flags
	vmDestroyCmd.Flags().String("name", "", "VM name (required)")
	vmDestroyCmd.Flags().String("data-dir", "~/.goship", "Data directory for VM disk images")
	vmDestroyCmd.Flags().Bool("keep-disk", false, "Keep disk image after destroying VM")
	_ = vmDestroyCmd.MarkFlagRequired("name")

	vmCmd.AddCommand(vmCreateCmd)
	vmCmd.AddCommand(vmDestroyCmd)
	vmCmd.AddCommand(vmListCmd)
}

func runVMCreate(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	baseImage, _ := cmd.Flags().GetString("base-image")
	memory, _ := cmd.Flags().GetInt64("memory")
	cpus, _ := cmd.Flags().GetInt("cpus")
	enableKVM, _ := cmd.Flags().GetBool("enable-kvm")
	networkType, _ := cmd.Flags().GetString("network-type")
	networkSource, _ := cmd.Flags().GetString("network-source")
	dataDir, _ := cmd.Flags().GetString("data-dir")

	baseImage = expandPath(baseImage)
	dataDir = expandPath(dataDir)

	mgr, cleanup, err := libvirtRuntime.NewVMManager(dataDir)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Fprintf(cmd.OutOrStdout(), "Creating VM %q...\n", name)

	info, err := mgr.Create(libvirtRuntime.CreateVMOptions{
		Name:          name,
		BaseImage:     baseImage,
		MemoryMB:      memory,
		CPUs:          cpus,
		EnableKVM:     enableKVM,
		SecurityNone:  true,
		NetworkType:   networkType,
		NetworkSource: networkSource,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nVM Created Successfully\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Name:   %s\n", info.Domain)
	fmt.Fprintf(cmd.OutOrStdout(), "  UUID:   %s\n", info.UUID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Memory: %d MB\n", info.Memory)
	fmt.Fprintf(cmd.OutOrStdout(), "  CPUs:   %d\n", info.CPUs)
	fmt.Fprintf(cmd.OutOrStdout(), "  Disk:   %s\n", info.Disk)

	return nil
}

func runVMDestroy(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	dataDir, _ := cmd.Flags().GetString("data-dir")
	keepDisk, _ := cmd.Flags().GetBool("keep-disk")

	dataDir = expandPath(dataDir)

	mgr, cleanup, err := libvirtRuntime.NewVMManager(dataDir)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Fprintf(cmd.OutOrStdout(), "Destroying VM: %s\n", name)

	result, err := mgr.Destroy(name, keepDisk)
	if err != nil {
		return err
	}

	if result.DiskRemoved {
		fmt.Fprintf(cmd.OutOrStdout(), "Removed disk directory: %s\n", result.DiskDir)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "VM %q destroyed successfully\n", name)
	return nil
}

func runVMList(cmd *cobra.Command, args []string) error {
	mgr, cleanup, err := libvirtRuntime.NewVMManager("")
	if err != nil {
		return err
	}
	defer cleanup()

	vms, err := mgr.List()
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%-30s  %s\n", "NAME", "STATE")
	fmt.Fprintf(cmd.OutOrStdout(), "%-30s  %s\n", strings.Repeat("-", 30), strings.Repeat("-", 15))

	if len(vms) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No GoShip VMs found.\n")
		return nil
	}

	for _, vm := range vms {
		fmt.Fprintf(cmd.OutOrStdout(), "%-30s  %s\n", vm.Domain, vm.State)
	}

	return nil
}

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
