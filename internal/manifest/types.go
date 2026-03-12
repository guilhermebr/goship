package manifest

import (
	"github.com/guilhermebr/goship/internal/shared/size"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// Manifest represents a goship.yml deployment manifest.
type Manifest struct {
	Version       string                 `yaml:"version"`
	Project       string                 `yaml:"project"`
	Resources     *Resources             `yaml:"resources,omitempty"`
	Domains       []string               `yaml:"domains,omitempty"`
	DefaultDomain string                 `yaml:"default_domain,omitempty"`
	Compose       *Compose               `yaml:"compose,omitempty"`
	Env           map[string]string      `yaml:"env,omitempty"`
	Secrets       map[string]string      `yaml:"secrets,omitempty"`
	SecretsFile   string                 `yaml:"secrets_file,omitempty"`
	Environments  map[string]Environment `yaml:"environments,omitempty"`
}

// Resources defines VM resource allocation in human-readable format.
type Resources struct {
	CPU    float64 `yaml:"cpu,omitempty"`
	Memory string  `yaml:"memory,omitempty"`
	Disk   string  `yaml:"disk,omitempty"`
}

// ToEntityResources converts manifest resources to domain entities.Resources.
func (r *Resources) ToEntityResources() (entities.Resources, error) {
	if r == nil {
		return entities.Resources{}, nil
	}

	var memoryMB, diskMB int64
	var err error

	if r.Memory != "" {
		memoryMB, err = size.ParseSizeMB(r.Memory)
		if err != nil {
			return entities.Resources{}, err
		}
	}

	if r.Disk != "" {
		diskMB, err = size.ParseSizeMB(r.Disk)
		if err != nil {
			return entities.Resources{}, err
		}
	}

	return entities.Resources{
		CPU:      r.CPU,
		MemoryMB: memoryMB,
		DiskMB:   diskMB,
	}, nil
}

// Compose defines the compose file reference.
type Compose struct {
	File  string `yaml:"file,omitempty"`
	Build *bool  `yaml:"build,omitempty"`
}

// Environment defines per-environment overrides.
type Environment struct {
	Resources *Resources        `yaml:"resources,omitempty"`
	Domains   []string          `yaml:"domains,omitempty"`
	Compose   *Compose          `yaml:"compose,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Secrets   map[string]string `yaml:"secrets,omitempty"`
}
