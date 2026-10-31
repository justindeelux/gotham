package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/composeguard"
)

const defaultManagedVolumeRoot = "/var/lib/gotham/volumes"

const envManagedVolumeRoot = "GOTHAM_MANAGED_VOLUME_ROOT"

// managedVolumeRoot mirrors internal/deploy, which imports this package and so
// cannot be reused here: services and applications confine binds to one tree.
func managedVolumeRoot() string {
	root := strings.TrimSpace(os.Getenv(envManagedVolumeRoot))
	if root == "" || !strings.HasPrefix(root, "/") {
		return defaultManagedVolumeRoot
	}
	return filepath.Clean(root)
}

// guardServiceDocument applies the same confinement rules as an application
// deploy to the rendered document. Service route labels are the only gotham.*
// labels a member may set.
func guardServiceDocument(serviceID uuid.UUID, document string) error {
	if err := composeguard.CheckNoInterpolation(document); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := composeguard.Validate(document, composeguard.Options{
		AppID:              serviceID.String(),
		ManagedRoot:        managedVolumeRoot(),
		ServiceRouteLabels: true,
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}
