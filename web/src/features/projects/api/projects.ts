import { http, teamHeaders } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import {
  parseCreatedProject,
  parseEnvironment,
  parseProject,
  parseProjectDetail,
  parseProjectList,
} from "@/features/projects/schemas/projects";

/**
 * Typed client for the project and environment routes served by
 * `internal/projects` (see the API contract in
 * docs/plans/13-projects-environments.md section 6):
 *
 *   GET    /projects                     POST   /projects
 *   GET    /projects/{id}                PATCH  /projects/{id}
 *   DELETE /projects/{id}
 *   POST   /projects/{id}/environments
 *   PATCH  /environments/{id}            DELETE /environments/{id}
 *
 * Every call is team-scoped through the optional `X-Team-Id` header (the
 * caller's personal team when absent), like the notification channels. An id
 * from another team answers 404; viewers read, members and admins write.
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * PE-4 runs against this contract before the backend lands: the calls below
 * follow it exactly and fail loudly (through describeProjectError) until
 * PE-1 is merged.
 */

/** Per-type resource counts carried by projects and environments. */
export interface ResourceCounts {
  applications: number;
  services: number;
  databases: number;
}

/** One project as the API returns it (Project in the contract). */
export interface Project {
  id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
  environment_count: number;
  resource_counts: ResourceCounts;
}

/** One environment as the API returns it (Environment in the contract). */
export interface Environment {
  id: string;
  project_id: string;
  name: string;
  created_at: string;
  updated_at: string;
  resource_counts: ResourceCounts;
}

/** Body accepted by POST /projects. */
export interface CreateProjectInput {
  name: string;
  description?: string;
}

/** Body accepted by PATCH /projects/{id}. Missing fields stay unchanged. */
export interface UpdateProjectInput {
  name?: string;
  description?: string;
}

/** Body accepted by POST /projects/{id}/environments and PATCH /environments/{id}. */
export interface EnvironmentNameInput {
  name: string;
}

/** A project with its environments, as GET /projects/{id} returns. */
export interface ProjectDetailEnvelope {
  project: Project;
  environments: Environment[];
}
/** A project with its fresh environments, as POST /projects returns. */
export interface CreatedProjectEnvelope {
  project: Project;
  environments: Environment[];
}

/** listProjects returns the active team's projects. */
export async function listProjects(teamId: string): Promise<Project[]> {
  const response = await http.get<unknown>("/projects", {
    headers: teamHeaders(teamId),
  });
  return parseProjectList(response.data).projects;
}

/** createProject stores a project; a `production` environment comes with it. */
export async function createProject(
  teamId: string,
  input: CreateProjectInput,
): Promise<CreatedProjectEnvelope> {
  const response = await http.post<unknown>("/projects", input, {
    headers: teamHeaders(teamId),
  });
  return parseCreatedProject(response.data);
}

/** getProject returns one project with its environments. */
export async function getProject(
  teamId: string,
  id: string,
): Promise<ProjectDetailEnvelope> {
  const response = await http.get<unknown>(`/projects/${id}`, {
    headers: teamHeaders(teamId),
  });
  return parseProjectDetail(response.data);
}

/** renameProject patches a project's name and/or description. */
export async function renameProject(
  teamId: string,
  id: string,
  input: UpdateProjectInput,
): Promise<Project> {
  const response = await http.patch<unknown>(
    `/projects/${id}`,
    input,
    { headers: teamHeaders(teamId) },
  );
  return parseProject(response.data).project;
}

/**
 * deleteProject deletes a project with its environments and shared
 * variables. A project with resources answers 409
 * (`project still has resources (including previews)`).
 */
export async function deleteProject(teamId: string, id: string): Promise<void> {
  await http.delete(`/projects/${id}`, { headers: teamHeaders(teamId) });
}

/** createEnvironment adds an environment to a project. */
export async function createEnvironment(
  teamId: string,
  projectId: string,
  input: EnvironmentNameInput,
): Promise<Environment> {
  const response = await http.post<unknown>(
    `/projects/${projectId}/environments`,
    input,
    { headers: teamHeaders(teamId) },
  );
  return parseEnvironment(response.data).environment;
}

/** renameEnvironment renames one environment. */
export async function renameEnvironment(
  teamId: string,
  id: string,
  input: EnvironmentNameInput,
): Promise<Environment> {
  const response = await http.patch<unknown>(
    `/environments/${id}`,
    input,
    { headers: teamHeaders(teamId) },
  );
  return parseEnvironment(response.data).environment;
}

/**
 * deleteEnvironment deletes an empty environment. A non-empty one answers
 * 409 (`environment still has resources (including previews)`); the last environment of a project
 * answers 409 (`a project needs at least one environment`).
 */
export async function deleteEnvironment(
  teamId: string,
  id: string,
): Promise<void> {
  await http.delete(`/environments/${id}`, { headers: teamHeaders(teamId) });
}

/** environmentResourceTotal counts every resource in one environment. */
export function environmentResourceTotal(environment: Environment): number {
  return (
    environment.resource_counts.applications +
    environment.resource_counts.services +
    environment.resource_counts.databases
  );
}

/** projectResourceTotal counts every resource across a project. */
export function projectResourceTotal(project: Project): number {
  return (
    project.resource_counts.applications +
    project.resource_counts.services +
    project.resource_counts.databases
  );
}

/**
 * resourceSummary renders counts as display text ("5 applications · 1
 * service · 2 databases"), matching the mockup's card line.
 */
export function resourceSummary(counts: ResourceCounts): string {
  const parts = [
    `${counts.applications} application${counts.applications === 1 ? "" : "s"}`,
    `${counts.services} service${counts.services === 1 ? "" : "s"}`,
    `${counts.databases} database${counts.databases === 1 ? "" : "s"}`,
  ];
  return parts.join(" · ");
}

/**
 * isNameTakenError reports whether an error is a 409 duplicate-name refusal
 * (`project/environment name already exists`). Dialogs render those inline
 * on the name field; every other failure stays in the dialog alert.
 */
export function isNameTakenError(error: unknown): boolean {
  return (
    isApiError(error) &&
    error.status === 409 &&
    stripErrorPrefix(error.message).includes("already exists")
  );
}

/**
 * describeProjectError maps a thrown error to a user-facing message. The
 * backend answers 400 for validation, 403 for an insufficient role, 404 for
 * an id from another team (or a removed row) and 409 for the duplicate-name
 * and non-empty delete protections, and its message is the actionable part.
 */
export function describeProjectError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 403) {
      return (
        stripErrorPrefix(error.message) ||
        "Your team role does not allow this action."
      );
    }
    if (error.status === 404) {
      return (
        stripErrorPrefix(error.message) ||
        "Not found. It may have been removed already."
      );
    }
    if (error.status === 409) {
      return (
        stripErrorPrefix(error.message) ||
        "The project changed while you were editing it. Reload and retry."
      );
    }
    if (error.status === 400) {
      return stripErrorPrefix(error.message) || "Invalid request.";
    }
    return stripErrorPrefix(error.message) || "Request failed";
  }
  if (error instanceof Error) {
    return (
      stripErrorPrefix(error.message) || "Something went wrong. Please try again."
    );
  }
  return "Something went wrong. Please try again.";
}
