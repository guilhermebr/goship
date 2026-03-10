package state

import "github.com/guilhermebr/goship/pkg/domain/entities"

// Storer defines the interface for state persistence.
type Storer interface {
	// Projects
	CreateProject(name string, runtime entities.RuntimeType, resources entities.Resources) (*entities.Project, error)
	GetProject(idOrName string) (*entities.Project, error)
	ListProjects() []*entities.Project
	UpdateProject(project *entities.Project) error
	DeleteProject(id string) error

	// Instances
	SetInstance(instance *entities.ProjectInstance) error
	GetInstance(projectID string) *entities.ProjectInstance
	GetInstanceByID(instanceID string) *entities.ProjectInstance
	UpdateInstance(instance *entities.ProjectInstance) error
	DeleteInstance(instanceID string) error

	// Apps
	SetApp(projectID string, app *entities.AppSpec) error
	GetApp(projectID, appName string) *entities.AppSpec
	GetApps(projectID string) []*entities.AppSpec
	DeleteApp(projectID, appName string) error

	// Nodes
	SetNode(node *entities.Node) error
	GetNode(idOrHostname string) (*entities.Node, error)
	ListNodes() []*entities.Node
	DeleteNode(idOrHostname string) error

	// Metadata
	Path() string
	DataDir() string
}
