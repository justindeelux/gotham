// Package projects manages project and environment grouping (Phase 13,
// PE-1): a project belongs to one team and holds 1..n environments, starting
// with production. Applications, services and databases attach to
// environments in PE-2; until then every resource count reads zero through
// the explicitly wired ZeroResourceCounter, which PE-2 replaces with the real
// counter.
package projects
