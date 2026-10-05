package projects

import (
	"errors"
)

// Sentinel errors mapped to HTTP statuses by the routes layer.
var (
	// ErrValidation — a name or description fails validation (400).
	ErrValidation = errors.New("projects: validation")
	// ErrNotFound — the project or environment does not exist or belongs to
	// another team (404).
	ErrNotFound = errors.New("projects: not found")
	// ErrForbidden — the caller's team role does not permit the mutation
	// (403).
	ErrForbidden = errors.New("projects: forbidden")
	// ErrProjectExists — a project name is already taken in the team (409
	// with the contract's exact body).
	ErrProjectExists = errors.New("projects: project name already exists")
	// ErrEnvironmentExists — an environment name is already taken in the
	// project (409).
	ErrEnvironmentExists = errors.New("projects: environment name already exists")
	// ErrProjectNotEmpty — the project still holds resources (409). The
	// message names previews because they block too while staying out of
	// every count.
	ErrProjectNotEmpty = errors.New("projects: project still has resources (including previews)")
	// ErrEnvironmentNotEmpty — the environment still holds resources (409;
	// see above).
	ErrEnvironmentNotEmpty = errors.New("projects: environment still has resources (including previews)")
	// ErrLastEnvironment — the project's only environment cannot be deleted
	// (409).
	ErrLastEnvironment = errors.New("projects: a project needs at least one environment")
)
