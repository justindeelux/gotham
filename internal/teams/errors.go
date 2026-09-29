package teams

import (
	"errors"
	"fmt"
)

// Sentinels the service and the HTTP layer map onto status codes. Domain
// packages translate ErrNotFound into their own not-found sentinel, so resource
// IDs outside the active team stay indistinguishable from missing ones.
var (
	// ErrValidation marks a bad request payload or parameter.
	ErrValidation = errors.New("teams: validation failed")
	// ErrNotFound marks an unknown team, member or invite.
	ErrNotFound = errors.New("teams: not found")
	// ErrForbidden marks a caller whose team role does not permit the action
	// (read_only mutations), or an invite that is not addressed to the caller.
	ErrForbidden = errors.New("teams: forbidden")
	// ErrConflict marks a membership or invite that already exists.
	ErrConflict = errors.New("teams: conflict")
	// ErrLastOwner marks an action that would leave a team without an owner.
	ErrLastOwner = errors.New("teams: the last owner cannot be removed or demoted")
	// ErrPersonalTeam marks an attempt to delete a personal team.
	ErrPersonalTeam = errors.New("teams: personal teams cannot be deleted")
	// ErrPersonalOwner marks an attempt to demote or remove the owner of a
	// personal team, whose membership is immutable.
	ErrPersonalOwner = errors.New("teams: the personal team's owner membership cannot be changed")
	// ErrTeamNotEmpty marks an attempt to delete a team that still owns
	// resources or nodes.
	ErrTeamNotEmpty = errors.New("teams: this team still owns resources")
	// ErrInviteExpired marks an invite past its expiry.
	ErrInviteExpired = errors.New("teams: this invite has expired")
	// ErrInviteUsed marks an invite that was already accepted.
	ErrInviteUsed = errors.New("teams: this invite has already been accepted")
)

// validateTeamName checks the display name of a team.
func validateTeamName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrValidation)
	}
	if len(name) > maxTeamNameLength {
		return "", fmt.Errorf("%w: name must be at most %d characters", ErrValidation, maxTeamNameLength)
	}
	return name, nil
}
