package deploy

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type runtimeAssociationRepository struct {
	Repository
	calls int
}

func (r *runtimeAssociationRepository) GetDeployment(ctx context.Context, appID, deploymentID uuid.UUID) (Deployment, error) {
	r.calls++
	return r.Repository.GetDeployment(ctx, appID, deploymentID)
}

func TestRuntimeDeploymentLabelWithoutRecordedContainer(t *testing.T) {
	for _, state := range []State{StateFailed, StateQueued} {
		t.Run(string(state), func(t *testing.T) {
			user := uuid.New()
			app := testApplication(user)
			deploymentID := uuid.New()
			container := runtimeFixture(app, fmt.Sprintf("%064x", 1), "exited", "")
			container.Labels[labelDeploymentID] = deploymentID.String()
			repo := &fakeRepository{app: app, deployments: []Deployment{{ID: deploymentID, ApplicationID: app.ID, State: state}}}
			svc := runtimeService(t, repo, &runtimeTestNode{inventory: []*agentv1.ContainerInfo{container}})
			out, err := svc.GetApplicationRuntime(context.Background(), user, app.ID)
			if err != nil || len(out.Containers) != 1 || out.Containers[0].DeploymentID == nil || *out.Containers[0].DeploymentID != deploymentID {
				t.Fatalf("managed deployment label did not establish association: %+v %v", out, err)
			}
			if out.Containers[0].State != "exited" || out.Containers[0].Health != "unknown" {
				t.Fatalf("association implied readiness: %+v", out)
			}
		})
	}
}

func TestRuntimeDeploymentLookupsBoundedAndCached(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	repo := &fakeRepository{app: app}
	inventory := []*agentv1.ContainerInfo{}
	deploymentIDs := []uuid.UUID{}
	for i := 0; i < 5; i++ {
		id := uuid.New()
		deploymentIDs = append(deploymentIDs, id)
		repo.deployments = append(repo.deployments, Deployment{ID: id, ApplicationID: app.ID})
		c := runtimeFixture(app, fmt.Sprintf("%064x", i+1), "running", "")
		c.Labels[labelDeploymentID] = id.String()
		inventory = append(inventory, c)
	}
	repeated := runtimeFixture(app, fmt.Sprintf("%064x", 6), "exited", "")
	repeated.Labels[labelDeploymentID] = deploymentIDs[0].String()
	inventory = append(inventory, repeated)
	svc := runtimeService(t, repo, &runtimeTestNode{inventory: inventory})
	counted := &runtimeAssociationRepository{Repository: repo}
	svc.repo = counted
	out, err := svc.GetApplicationRuntime(context.Background(), user, app.ID)
	if err != nil || len(out.Containers) != 6 || counted.calls != 4 || out.Coverage != "complete" {
		t.Fatalf("lookups=%d runtime=%+v %v", counted.calls, out, err)
	}
	for i, row := range out.Containers {
		if i == 4 {
			if row.DeploymentID != nil {
				t.Fatal("unqueried deployment association asserted")
			}
			continue
		}
		if row.DeploymentID == nil {
			t.Fatalf("checked association lost: %+v", row)
		}
	}
}

func TestRuntimeMissingDeploymentLookupCached(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	repo := &fakeRepository{app: app}
	id := uuid.New()
	inventory := []*agentv1.ContainerInfo{}
	for i := 0; i < 5; i++ {
		c := runtimeFixture(app, fmt.Sprintf("%064x", i+1), "exited", "")
		c.Labels[labelDeploymentID] = id.String()
		inventory = append(inventory, c)
	}
	svc := runtimeService(t, repo, &runtimeTestNode{inventory: inventory})
	counted := &runtimeAssociationRepository{Repository: repo}
	svc.repo = counted
	out, err := svc.GetApplicationRuntime(context.Background(), user, app.ID)
	if err != nil || counted.calls != 1 {
		t.Fatalf("missing lookup repeated %d times: %v", counted.calls, err)
	}
	for _, row := range out.Containers {
		if row.DeploymentID != nil {
			t.Fatal("missing deployment associated")
		}
	}
}
