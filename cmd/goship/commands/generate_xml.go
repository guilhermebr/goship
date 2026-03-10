package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// generateXMLCmd generates libvirt domain XML from flags.
var generateXMLCmd = &cobra.Command{
	Use:   "generate-xml",
	Short: "Generate libvirt domain XML",
	Long: `Generates a libvirt domain XML definition from the provided flags. No libvirt connection required.
Useful for experimenting with VM configurations.`,
	RunE: runGenerateXML,
}

func init() {
	generateXMLCmd.Flags().String("name", "goship-vm", "VM name")
	generateXMLCmd.Flags().String("uuid", "", "VM UUID (auto-generated if empty)")
	generateXMLCmd.Flags().Int64("memory", 512, "Memory in MB")
	generateXMLCmd.Flags().Int("cpus", 1, "Number of CPU cores")
	generateXMLCmd.Flags().Bool("enable-kvm", true, "Enable KVM acceleration")
	generateXMLCmd.Flags().String("disk-path", "/var/lib/goship/disk.qcow2", "Disk image path")
	generateXMLCmd.Flags().String("disk-format", "qcow2", "Disk format")
	generateXMLCmd.Flags().String("network-type", "network", "Network type (network, bridge, user)")
	generateXMLCmd.Flags().String("network-source", "default", "Network source name")
}

func runGenerateXML(cmd *cobra.Command, args []string) error {
	name, err := cmd.Flags().GetString("name")
	if err != nil {
		return fmt.Errorf("invalid --name flag: %w", err)
	}
	uuid, err := cmd.Flags().GetString("uuid")
	if err != nil {
		return fmt.Errorf("invalid --uuid flag: %w", err)
	}
	memory, err := cmd.Flags().GetInt64("memory")
	if err != nil {
		return fmt.Errorf("invalid --memory flag: %w", err)
	}
	cpus, err := cmd.Flags().GetInt("cpus")
	if err != nil {
		return fmt.Errorf("invalid --cpus flag: %w", err)
	}
	enableKVM, err := cmd.Flags().GetBool("enable-kvm")
	if err != nil {
		return fmt.Errorf("invalid --enable-kvm flag: %w", err)
	}
	diskPath, err := cmd.Flags().GetString("disk-path")
	if err != nil {
		return fmt.Errorf("invalid --disk-path flag: %w", err)
	}
	diskFormat, err := cmd.Flags().GetString("disk-format")
	if err != nil {
		return fmt.Errorf("invalid --disk-format flag: %w", err)
	}
	networkType, err := cmd.Flags().GetString("network-type")
	if err != nil {
		return fmt.Errorf("invalid --network-type flag: %w", err)
	}
	networkSource, err := cmd.Flags().GetString("network-source")
	if err != nil {
		return fmt.Errorf("invalid --network-source flag: %w", err)
	}

	if uuid == "" {
		var uuidErr error
		uuid, uuidErr = libvirt.GenerateUUID()
		if uuidErr != nil {
			return fmt.Errorf("failed to generate UUID: %w", uuidErr)
		}
	}

	config := &libvirt.DomainConfig{
		Name:      name,
		UUID:      uuid,
		MemoryMB:  memory,
		EnableKVM: enableKVM,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   cpus,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{
				Path:   diskPath,
				Format: diskFormat,
				Bus:    "virtio",
			},
		},
		Networks: []entities.NetworkDevice{
			{
				Type:   networkType,
				Source: networkSource,
				Model:  "virtio",
			},
		},
	}

	xml, err := libvirt.GenerateDomainXML(config)
	if err != nil {
		return fmt.Errorf("failed to generate domain XML: %w", err)
	}

	fmt.Print(xml)
	return nil
}
