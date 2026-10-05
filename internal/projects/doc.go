// Package projects manages project and environment grouping (Phase 13):
// a project belongs to one team and holds 1..n environments, starting with
// production. Applications, services and databases attach to environments;
// StoreCounter tallies them per project and environment, and the resources
// surface lists one environment's workloads through the domain services.
package projects
