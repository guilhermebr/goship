package commands

import (
	"crypto/rand"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// generateXMLCmd generates libvirt domain XML from flags.
var generateXMLCmd = &cobra.Command{
	Use:   "generate-xml",
	Short: "Generate libvirt domain XML",
	Long:  `Generates a libvirt domain XML definition from the provided flags. No libvirt connection required. Useful for experimenting with VM configurations.`,
	RunE:  runGenerateXML,
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
	name, _ := cmd.Flags().GetString("name")
	uuid, _ := cmd.Flags().GetString("uuid")
	memory, _ := cmd.Flags().GetInt64("memory")
	cpus, _ := cmd.Flags().GetInt("cpus")
	enableKVM, _ := cmd.Flags().GetBool("enable-kvm")
	diskPath, _ := cmd.Flags().GetString("disk-path")
	diskFormat, _ := cmd.Flags().GetString("disk-format")
	networkType, _ := cmd.Flags().GetString("network-type")
	networkSource, _ := cmd.Flags().GetString("network-source")

	if uuid == "" {
		var err error
		uuid, err = generateUUID()
		if err != nil {
			return fmt.Errorf("failed to generate UUID: %w", err)
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

// generateUUID generates a v4 UUID using crypto/rand.
func generateUUID() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	// Set version 4 (bits 12-15 of time_hi_and_version)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant (bits 6-7 of clock_seq_hi_and_reserved)
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}
