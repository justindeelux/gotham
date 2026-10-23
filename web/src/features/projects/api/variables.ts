import { http, teamHeaders } from "@/shared/api/http";

import { parseSharedVariables } from "@/features/projects/schemas/variables";
import type {
  SharedVariable,
  SharedVariableWrite,
} from "@/features/projects/schemas/variables";

export async function getProjectVariables(
  teamId: string,
  projectId: string,
): Promise<SharedVariable[]> {
  const response = await http.get<unknown>(`/projects/${projectId}/variables`, {
    headers: teamHeaders(teamId),
  });
  return parseSharedVariables(response.data).variables;
}

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
