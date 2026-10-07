import { http, teamHeaders } from "@/shared/api/http";
import { activeLocale, i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import type { Service } from "@/features/services/api/services";
import {
  parseCreatedProject,
  parseEnvironment,
  parseEnvironmentResources,
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

/**
 * One application of GET /environments/{id}/resources: the deploy list item
 * plus the grouping fields and the node name (mirrors
 * environmentResourceApplication in internal/projects/routes.go).
 */
export interface EnvironmentResourceApplication {
  id: string;
  name: string;
  environment_id: string;
  environment_name: string;
  project_id: string;
  project_name: string;
  provider: string;
  repo: string;
  clone_url: string;
  branch: string;
  build_pack: string;
  base_domain: string;
  base_domain_disabled: boolean;
  port: number;
  host_port: number;
  server_id: string;
  server_name: string;
  /** True for a PR-preview sibling (only present with ?previews=1). */
  is_preview: boolean;
  /** Base application id of a preview, empty when none. */
  preview_of?: string;
  created_at: string;
  updated_at: string;
}

/**
 * One database of GET /environments/{id}/resources: the databases list item
 * plus the grouping fields and the node name (mirrors
 * environmentResourceDatabase in internal/projects/routes.go).
 */
export interface EnvironmentResourceDatabase {
  id: string;
  name: string;
  environment_id: string;
  environment_name: string;
  project_id: string;
  project_name: string;
  engine: string;
  version?: string;
  status: string;
  server_id: string;
  server_name: string;
  public_port: number;
  volume: string;
  created_at: string;
  updated_at: string;
}

/**
 * GET /environments/{id}/resources as the environment page consumes it. The
 * services ride the services response shape unchanged (see ServiceResponse
 * in internal/services/response.go).
 */
export interface EnvironmentResources {
  environment: Environment;
  project: Project;
  applications: EnvironmentResourceApplication[];
  services: Service[];
  databases: EnvironmentResourceDatabase[];
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

/**
 * getEnvironmentResources returns one environment with its project and the
 * workloads attached to it (GET /environments/{id}/resources → 200).
 * Previews stay out of the default listing; `includePreviews` adds them
 * (?previews=1) for the environment page's preview switch.
 */
export async function getEnvironmentResources(
  teamId: string,
  id: string,
  includePreviews = false,
): Promise<EnvironmentResources> {
  const response = await http.get<unknown>(`/environments/${id}/resources`, {
    headers: teamHeaders(teamId),
    params: includePreviews ? { previews: "1" } : {},
  });
  // Warn-only envelope check: the services ride their own response shape, so
  // the parsed result is adopted as the resources view.
  return parseEnvironmentResources(response.data) as unknown as EnvironmentResources;
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
 * service · 2 databases"), matching the mockup's card line. Kind labels
 * resolve in the active locale at invocation time so template callers
 * refresh on a language switch; English output is unchanged.
 */
export function resourceSummary(counts: ResourceCounts): string {
  // Tracks the locale when called during render or inside a computed.
  void activeLocale.value;
  const unit = (
    one: string,
    other: string,
    count: number,
  ): string =>
    String(i18n.global.t(count === 1 ? one : other, { count }));
  const parts = [
    unit(
      "projects.counts.applicationsOne",
      "projects.counts.applicationsOther",
      counts.applications,
    ),
    unit(
      "projects.counts.servicesOne",
      "projects.counts.servicesOther",
      counts.services,
    ),
    unit(
      "projects.counts.databasesOne",
      "projects.counts.databasesOther",
      counts.databases,
    ),
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
 * withDiagnostic pairs an unknown failure's raw diagnostic with a localized
 * summary (`<summary>: <raw>`). An empty or already-generic diagnostic
 * renders the summary alone. Known refusal branches below keep their raw
 * actionable text untouched.
 */
function withDiagnostic(summary: string, raw: string): string {
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

/**
 * describeProjectError maps a thrown error to a user-facing message. The
 * backend answers 400 for validation, 403 for an insufficient role, 404 for
 * an id from another team (or a removed row) and 409 for the duplicate-name
 * and non-empty delete protections, and its message is the actionable part.
 *
 * Classification still runs on the raw error (status plus the stripped
 * server message, exactly as before); only the curated fallback summaries
 * resolve in the active locale. Actionable server text passes through
 * untouched so secrets stay redacted and diagnostics stay intact.
 */
export function describeProjectError(error: unknown): string {
  // Tracks the locale when called during render or inside a computed, so
  // retained failures refresh on a language switch.
  void activeLocale.value;
  const text = (
    key: string,
    params?: Record<string, string | number>,
  ): string => String(i18n.global.t(key, params ?? {}));
  if (isApiError(error)) {
    if (error.status === 401) {
      return text("projects.errors.sessionExpired");
    }
    if (error.status === 403) {
      return (
        stripErrorPrefix(error.message) ||
        text("projects.errors.forbiddenFallback")
      );
    }
    if (error.status === 404) {
      return (
        stripErrorPrefix(error.message) ||
        text("projects.errors.notFoundFallback")
      );
    }
    if (error.status === 409) {
      return (
        stripErrorPrefix(error.message) ||
        text("projects.errors.conflictFallback")
      );
    }
    if (error.status === 400) {
      return stripErrorPrefix(error.message) || text("projects.errors.badRequestFallback");
    }
    return withDiagnostic(
      text("common.errors.requestFailed"),
      stripErrorPrefix(error.message),
    );
  }
  if (error instanceof Error) {
    return withDiagnostic(
      text("common.errors.unexpected"),
      stripErrorPrefix(error.message),
    );
  }
  return text("common.errors.unexpected");
}
