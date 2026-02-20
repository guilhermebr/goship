package libvirt

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestDefaultNetworkXML_Valid(t *testing.T) {
	var network struct {
		XMLName xml.Name `xml:"network"`
		Name    string   `xml:"name"`
		Bridge  struct {
			Name string `xml:"name,attr"`
			STP  string `xml:"stp,attr"`
		} `xml:"bridge"`
		Forward struct {
			Mode string `xml:"mode,attr"`
		} `xml:"forward"`
		IP struct {
			Address string `xml:"address,attr"`
			Netmask string `xml:"netmask,attr"`
			DHCP    struct {
				Range struct {
					Start string `xml:"start,attr"`
					End   string `xml:"end,attr"`
				} `xml:"range"`
			} `xml:"dhcp"`
		} `xml:"ip"`
	}

	if err := xml.Unmarshal([]byte(defaultNetworkXML), &network); err != nil {
		t.Fatalf("defaultNetworkXML is not valid XML: %v", err)
	}

	if network.Name != "default" {
		t.Errorf("network name = %q, want %q", network.Name, "default")
	}
	if network.Bridge.Name != "virbr0" {
		t.Errorf("bridge name = %q, want %q", network.Bridge.Name, "virbr0")
	}
	if network.Forward.Mode != "nat" {
		t.Errorf("forward mode = %q, want %q", network.Forward.Mode, "nat")
	}
	if network.IP.Address != "192.168.122.1" {
		t.Errorf("IP address = %q, want %q", network.IP.Address, "192.168.122.1")
	}
	if network.IP.Netmask != "255.255.255.0" {
		t.Errorf("netmask = %q, want %q", network.IP.Netmask, "255.255.255.0")
	}
	if network.IP.DHCP.Range.Start != "192.168.122.2" {
		t.Errorf("DHCP range start = %q, want %q", network.IP.DHCP.Range.Start, "192.168.122.2")
	}
	if network.IP.DHCP.Range.End != "192.168.122.254" {
		t.Errorf("DHCP range end = %q, want %q", network.IP.DHCP.Range.End, "192.168.122.254")
	}
}

func TestDefaultNetworkXML_ContainsExpectedElements(t *testing.T) {
	expected := []string{
		"<name>default</name>",
		"bridge name='virbr0'",
		"forward mode='nat'",
		"<range start='192.168.122.2' end='192.168.122.254'/>",
	}

	for _, s := range expected {
		if !strings.Contains(defaultNetworkXML, s) {
			t.Errorf("defaultNetworkXML missing expected element: %s", s)
		}
	}
}
