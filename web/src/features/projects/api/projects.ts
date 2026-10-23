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

export interface ResourceCounts {
  applications: number;
  services: number;
  databases: number;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
  environment_count: number;
  resource_counts: ResourceCounts;
}

export interface Environment {
  id: string;
  project_id: string;
  name: string;
  created_at: string;
  updated_at: string;
  resource_counts: ResourceCounts;
}

export interface CreateProjectInput {
  name: string;
  description?: string;
}

export interface UpdateProjectInput {
  name?: string;
  description?: string;
}

export interface EnvironmentNameInput {
  name: string;
}

export interface ProjectDetailEnvelope {
  project: Project;
  environments: Environment[];
}
export interface CreatedProjectEnvelope {
  project: Project;
  environments: Environment[];
}

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
  source_type?: string;
  branch: string;
  build_pack: string;
  primary_domain: string;
  server_id: string;
  server_name: string;
  is_preview: boolean;
  preview_of?: string;
  created_at: string;
  updated_at: string;
}

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

export interface EnvironmentResources {
  environment: Environment;
  project: Project;
  applications: EnvironmentResourceApplication[];
  services: Service[];
  databases: EnvironmentResourceDatabase[];
}

export async function listProjects(teamId: string): Promise<Project[]> {
  const response = await http.get<unknown>("/projects", {
    headers: teamHeaders(teamId),
  });
  return parseProjectList(response.data).projects;
}

export async function createProject(
  teamId: string,
  input: CreateProjectInput,
): Promise<CreatedProjectEnvelope> {
  const response = await http.post<unknown>("/projects", input, {
    headers: teamHeaders(teamId),
  });
  return parseCreatedProject(response.data);
}

export async function getProject(
  teamId: string,
  id: string,
): Promise<ProjectDetailEnvelope> {
  const response = await http.get<unknown>(`/projects/${id}`, {
    headers: teamHeaders(teamId),
  });
  return parseProjectDetail(response.data);
}

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

export async function deleteProject(teamId: string, id: string): Promise<void> {
  await http.delete(`/projects/${id}`, { headers: teamHeaders(teamId) });
}

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

export async function deleteEnvironment(
  teamId: string,
  id: string,
): Promise<void> {
  await http.delete(`/environments/${id}`, { headers: teamHeaders(teamId) });
}

export async function getEnvironmentResources(
  teamId: string,
  id: string,
  includePreviews = false,
): Promise<EnvironmentResources> {
  const response = await http.get<unknown>(`/environments/${id}/resources`, {
    headers: teamHeaders(teamId),
    params: includePreviews ? { previews: "1" } : {},
  });
  return parseEnvironmentResources(response.data) as unknown as EnvironmentResources;
}

export function environmentResourceTotal(environment: Environment): number {
  return (
    environment.resource_counts.applications +
    environment.resource_counts.services +
    environment.resource_counts.databases
  );
}

export function projectResourceTotal(project: Project): number {
  return (
    project.resource_counts.applications +
    project.resource_counts.services +
    project.resource_counts.databases
  );
}

export function resourceSummary(counts: ResourceCounts): string {
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

export function isNameTakenError(error: unknown): boolean {
  return (
    isApiError(error) &&
    error.status === 409 &&
    stripErrorPrefix(error.message).includes("already exists")
  );
}

function withDiagnostic(summary: string, raw: string): string {
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

export function describeProjectError(error: unknown): string {
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
