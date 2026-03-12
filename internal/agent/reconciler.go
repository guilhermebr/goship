package agent

import (
	"context"
	"log"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	apiserver "github.com/guilhermebr/goship/internal/api"
)

// Reconciler compares desired state with actual state and takes corrective actions.
type Reconciler struct {
	rt     runtime.ProjectRuntime
	logger *log.Logger
}

// Reconcile compares desired state with actual instances and creates missing
// instances and deploys missing apps.
func (r *Reconciler) Reconcile(ctx context.Context, desired *apiserver.DesiredState) error {
	if r.rt == nil {
		return nil
	}

	// Get actual instances from the runtime.
	actual, err := r.rt.ListInstances(ctx)
	if err != nil {
		return err
	}

	// Build a set of existing instance project IDs.
	existingProjects := make(map[string]string) // projectID -> instanceID
	for _, inst := range actual {
		existingProjects[inst.ProjectID] = inst.ID
	}

	// Create missing instances for desired projects.
	for _, project := range desired.Projects {
		if _, exists := existingProjects[project.ID]; exists {
			continue
		}

		r.logger.Printf("reconcile: creating instance for project %s", project.Name)
		inst, err := r.rt.CreateInstance(ctx, project)
		if err != nil {
			r.logger.Printf("reconcile: failed to create instance for project %s: %v", project.Name, err)
			continue
		}
		existingProjects[project.ID] = inst.ID
	}

	// Deploy missing apps.
	for projectID, apps := range desired.Apps {
		instanceID, exists := existingProjects[projectID]
		if !exists {
			continue
		}

		for _, app := range apps {
			r.logger.Printf("reconcile: deploying app %s in project %s", app.Name, projectID)
			if err := r.rt.DeployApp(ctx, instanceID, app); err != nil {
				r.logger.Printf("reconcile: failed to deploy app %s: %v", app.Name, err)
			}
		}
	}

	return nil
}
