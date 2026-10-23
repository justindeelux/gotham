package projects

import (
	"errors"
)

var (
	ErrValidation          = errors.New("projects: validation")
	ErrNotFound            = errors.New("projects: not found")
	ErrForbidden           = errors.New("projects: forbidden")
	ErrProjectExists       = errors.New("projects: project name already exists")
	ErrEnvironmentExists   = errors.New("projects: environment name already exists")
	ErrProjectNotEmpty     = errors.New("projects: project still has resources (including previews)")
	ErrEnvironmentNotEmpty = errors.New("projects: environment still has resources (including previews)")
	ErrLastEnvironment     = errors.New("projects: a project needs at least one environment")
)
