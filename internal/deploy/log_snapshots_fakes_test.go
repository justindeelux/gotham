package deploy

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

func (r *fakeRepository) GetBoundedBuildLog(ctx context.Context, appID, deploymentID uuid.UUID, options LogOptions) (string, State, bool, error) {
	dep, err := r.GetDeployment(ctx, appID, deploymentID)
	if err != nil {
		return "", "", false, err
	}
	lines := strings.Split(dep.BuildLog, "\n")
	count := len(lines)
	if strings.HasSuffix(dep.BuildLog, "\n") {
		count--
	}
	raw := strings.Join(lines[max(0, count-options.Tail):], "\n")
	return raw, dep.State, len(raw) > options.MaxBytes, nil
}
func (f *fakeDeployService) GetApplicationLogs(_ context.Context, userID, appID uuid.UUID, _ LogOptions) (ApplicationLogs, error) {
	f.seenUser, f.seenApplication = userID, appID
	return ApplicationLogs{ApplicationID: appID, Source: "runtime", Containers: []ContainerLog{}}, f.getErr
}
func (f *fakeDeployService) GetDeploymentLogs(_ context.Context, userID, appID, deploymentID uuid.UUID, _ LogOptions) (DeploymentLogs, error) {
	f.seenUser, f.seenApplication = userID, appID
	return DeploymentLogs{ApplicationID: appID, DeploymentID: deploymentID, Source: "persisted_build"}, f.getErr
}
