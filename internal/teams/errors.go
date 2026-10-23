package teams

import (
	"errors"
	"fmt"
)

var (
	ErrValidation    = errors.New("teams: validation failed")
	ErrNotFound      = errors.New("teams: not found")
	ErrForbidden     = errors.New("teams: forbidden")
	ErrConflict      = errors.New("teams: conflict")
	ErrLastOwner     = errors.New("teams: the last owner cannot be removed or demoted")
	ErrPersonalTeam  = errors.New("teams: personal teams cannot be deleted")
	ErrPersonalOwner = errors.New("teams: the personal team's owner membership cannot be changed")
	ErrTeamNotEmpty  = errors.New("teams: this team still owns resources")
	ErrInviteExpired = errors.New("teams: this invite has expired")
	ErrInviteUsed    = errors.New("teams: this invite has already been accepted")
)

func validateTeamName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrValidation)
	}
	if len(name) > maxTeamNameLength {
		return "", fmt.Errorf("%w: name must be at most %d characters", ErrValidation, maxTeamNameLength)
	}
	return name, nil
}
