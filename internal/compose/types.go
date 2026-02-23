package compose

// ComposeFile represents a docker-compose.yml file.
type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeService represents a single service in a compose file.
type ComposeService struct {
	Image       string            `yaml:"image"`
	Command     any               `yaml:"command"`
	Ports       []string          `yaml:"ports"`
	Environment any               `yaml:"environment"`
	Volumes     []string          `yaml:"volumes"`
	Restart     string            `yaml:"restart"`
	Deploy      *ComposeDeploy    `yaml:"deploy"`
	DependsOn   any               `yaml:"depends_on"`
	Networks    any               `yaml:"networks"`
	Build       any               `yaml:"build"`
	Labels      map[string]string `yaml:"labels"`
}

// ComposeDeploy represents the deploy section of a compose service.
type ComposeDeploy struct {
	Resources *ComposeResources `yaml:"resources"`
}

// ComposeResources represents the resources section of a compose deploy.
type ComposeResources struct {
	Limits *ComposeResourceSpec `yaml:"limits"`
}

// ComposeResourceSpec represents CPU and memory resource limits.
type ComposeResourceSpec struct {
	CPUs   string `yaml:"cpus"`
	Memory string `yaml:"memory"`
}

// BuildContext holds parsed build information for a compose service.
type BuildContext struct {
	Context    string // Build context directory
	Dockerfile string // Dockerfile path (relative to context)
	ImageName  string // Generated image name: goship-<service>:latest
}
