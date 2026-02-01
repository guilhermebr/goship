package libvirt

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// DomainConfig contains configuration for generating domain XML.
type DomainConfig struct {
	Name          string
	UUID          string
	MemoryMB      int64
	CPU           entities.CPUTopology
	EnableKVM     bool
	Disks         []entities.DiskDevice
	CDROMs        []CDROMDevice
	Networks      []entities.NetworkDevice
	Serials       []entities.SerialDevice
	MemoryBacking *entities.MemoryBacking
	SecurityNone  bool // Disable libvirt security confinement (seclabel type='none')
}

// CDROMDevice defines a CDROM device (for cloud-init ISO).
type CDROMDevice struct {
	Path   string
	Format string
}

// TotalVCPUs returns the total number of vCPUs.
func (c *DomainConfig) TotalVCPUs() int {
	return c.CPU.TotalVCPUs()
}

const domainXMLTemplate = `<domain type='{{if .EnableKVM}}kvm{{else}}qemu{{end}}'>
  <name>{{.Name}}</name>
  <uuid>{{.UUID}}</uuid>
  <memory unit='MiB'>{{.MemoryMB}}</memory>
  <currentMemory unit='MiB'>{{.MemoryMB}}</currentMemory>
  <vcpu placement='static'>{{.TotalVCPUs}}</vcpu>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
  </features>
  <cpu mode='host-passthrough'>
    <topology sockets='{{.CPU.Sockets}}' cores='{{.CPU.Cores}}' threads='{{.CPU.Threads}}'/>
  </cpu>
{{- if .MemoryBacking}}
  <memoryBacking>
{{- if .MemoryBacking.Hugepages}}
    <hugepages/>
{{- end}}
{{- if .MemoryBacking.Locked}}
    <locked/>
{{- end}}
  </memoryBacking>
{{- end}}
  <clock offset='utc'>
    <timer name='rtc' tickpolicy='catchup'/>
    <timer name='pit' tickpolicy='delay'/>
    <timer name='hpet' present='no'/>
  </clock>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
{{- range $i, $disk := .Disks}}
    <disk type='file' device='disk'>
      <driver name='qemu' type='{{$disk.Format}}'{{if $disk.Cache}} cache='{{$disk.Cache}}'{{end}}/>
      <source file='{{$disk.Path}}'/>
      <target dev='vd{{diskLetter $i}}' bus='{{$disk.Bus}}'/>
{{- if $disk.ReadOnly}}
      <readonly/>
{{- end}}
    </disk>
{{- end}}
{{- range $i, $cdrom := .CDROMs}}
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='{{if $cdrom.Format}}{{$cdrom.Format}}{{else}}raw{{end}}'/>
      <source file='{{$cdrom.Path}}'/>
      <target dev='sd{{diskLetter $i}}' bus='sata'/>
      <readonly/>
    </disk>
{{- end}}
{{- range .Networks}}
{{- if eq .Type "network"}}
    <interface type='network'>
      <source network='{{.Source}}'/>
{{- if .MACAddress}}
      <mac address='{{.MACAddress}}'/>
{{- end}}
      <model type='{{if .Model}}{{.Model}}{{else}}virtio{{end}}'/>
    </interface>
{{- else if eq .Type "bridge"}}
    <interface type='bridge'>
      <source bridge='{{.Source}}'/>
{{- if .MACAddress}}
      <mac address='{{.MACAddress}}'/>
{{- end}}
      <model type='{{if .Model}}{{.Model}}{{else}}virtio{{end}}'/>
    </interface>
{{- else if eq .Type "user"}}
    <interface type='user'>
{{- if .MACAddress}}
      <mac address='{{.MACAddress}}'/>
{{- end}}
      <model type='{{if .Model}}{{.Model}}{{else}}virtio{{end}}'/>
    </interface>
{{- end}}
{{- end}}
{{- range .Serials}}
    <channel type='unix'>
      <source mode='bind' path='{{.SocketPath}}'/>
      <target type='virtio' name='{{.PortName}}'/>
    </channel>
{{- end}}
    <serial type='pty'>
      <target port='0'/>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <rng model='virtio'>
      <backend model='random'>/dev/urandom</backend>
    </rng>
    <memballoon model='virtio'>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x09' function='0x0'/>
    </memballoon>
  </devices>
{{- if .SecurityNone}}
  <seclabel type='none'/>
{{- end}}
</domain>`

// diskLetter returns the disk letter for the given index (a, b, c, ...).
func diskLetter(i int) string {
	return string(rune('a' + i))
}

// GenerateDomainXML generates libvirt domain XML for a VM configuration.
func GenerateDomainXML(config *DomainConfig) (string, error) {
	funcMap := template.FuncMap{
		"diskLetter": diskLetter,
	}

	tmpl, err := template.New("domain").Funcs(funcMap).Parse(domainXMLTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse domain template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return "", fmt.Errorf("failed to execute domain template: %w", err)
	}

	return buf.String(), nil
}
