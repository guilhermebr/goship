package size

import (
	"testing"
)

func TestParseSizeMB(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		// Plain integers (backward compat: treated as MB).
		{"512", 512, false},
		{"0", 0, false},
		{"1024", 1024, false},

		// Megabyte suffix.
		{"512M", 512, false},
		{"512m", 512, false},
		{"1024M", 1024, false},

		// Gigabyte suffix.
		{"8G", 8192, false},
		{"8g", 8192, false},
		{"1G", 1024, false},
		{"2G", 2048, false},

		// Whitespace trimming.
		{"  512M  ", 512, false},
		{" 8G ", 8192, false},

		// Error cases.
		{"", 0, true},
		{"abc", 0, true},
		{"-1M", 0, true},
		{"-1G", 0, true},
		{"-1", 0, true},
		{"M", 0, true},
		{"G", 0, true},
		{"1.5G", 0, true},
		{"1T", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSizeMB(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSizeMB(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSizeMB(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
