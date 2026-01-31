package commands

import "testing"

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   uint32
		want    string
	}{
		{name: "zero", input: 0, want: "0.0.0"},
		{name: "major only", input: 10000000, want: "10.0.0"},
		{name: "minor only", input: 5000, want: "0.5.0"},
		{name: "patch only", input: 3, want: "0.0.3"},
		{name: "libvirt 10.8.0", input: 10008000, want: "10.8.0"},
		{name: "qemu 8.2.7", input: 8002007, want: "8.2.7"},
		{name: "all parts", input: 1002003, want: "1.2.3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.input)
			if got != tt.want {
				t.Errorf("formatVersion(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
