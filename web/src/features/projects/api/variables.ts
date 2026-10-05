import { http, teamHeaders } from "@/shared/api/http";

import { parseSharedVariables } from "@/features/projects/schemas/variables";
import type {
  SharedVariable,
  SharedVariableWrite,
} from "@/features/projects/schemas/variables";

/**
 * Typed client for the shared-variables routes served by
 * `internal/projects` (see the API contract in
 * docs/plans/13-projects-environments.md section 6):
 *
 *   GET /projects/{id}/variables        PUT /projects/{id}/variables
 *   GET /environments/{id}/variables    PUT /environments/{id}/variables
 *
 * Secret values are write-only: responses never carry them (the `value`
 * field is omitted for secrets). A PUT replaces the whole set; an omitted
 * `value` for an existing secret key keeps its sealed ciphertext, while a
 * secret without a value for a new key is a 400. Every call is team-scoped
 * through `X-Team-Id`, like the project routes.
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`).
 */

/** getProjectVariables returns one project's project-level variables. */
export async function getProjectVariables(
  teamId: string,
  projectId: string,
): Promise<SharedVariable[]> {
  const response = await http.get<unknown>(`/projects/${projectId}/variables`, {
    headers: teamHeaders(teamId),
  });
  return parseSharedVariables(response.data).variables;
}

/**
 * replaceProjectVariables replaces one project's whole project-level set and
 * returns it masked (secrets without values).
 */
export async function replaceProjectVariables(
  teamId: string,
  projectId: string,
  variables: SharedVariableWrite[],
): Promise<SharedVariable[]> {
  const response = await http.put<unknown>(
    `/projects/${projectId}/variables`,
    { variables },
    { headers: teamHeaders(teamId) },
  );
  return parseSharedVariables(response.data).variables;
}

/** getEnvironmentVariables returns one environment's variables. */
export async function getEnvironmentVariables(
  teamId: string,
  environmentId: string,
): Promise<SharedVariable[]> {
  const response = await http.get<unknown>(
    `/environments/${environmentId}/variables`,
    { headers: teamHeaders(teamId) },
  );
  return parseSharedVariables(response.data).variables;
}

/**
 * replaceEnvironmentVariables replaces one environment's whole set and
 * returns it masked (secrets without values).
 */
export async function replaceEnvironmentVariables(
  teamId: string,
  environmentId: string,
  variables: SharedVariableWrite[],
): Promise<SharedVariable[]> {
  const response = await http.put<unknown>(
    `/environments/${environmentId}/variables`,
    { variables },
    { headers: teamHeaders(teamId) },
  );
  return parseSharedVariables(response.data).variables;
}
