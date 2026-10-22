package deploy

import (
	"context"

	"github.com/google/uuid"
)

func (f *fakeDeployService) GetApplicationRuntime(_ context.Context, userID, appID uuid.UUID) (ApplicationRuntime, error) {
	f.seenUser, f.seenApplication = userID, appID
	return ApplicationRuntime{ApplicationID: appID, Coverage: "unavailable", Containers: []RuntimeContainer{}}, f.getErr
}
