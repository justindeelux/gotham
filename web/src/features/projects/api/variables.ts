import { http } from "@/shared/api/http";

import { parseSharedVariables } from "@/features/projects/schemas/variables";
import type {
  SharedVariable,
  SharedVariableWrite,
} from "@/features/projects/schemas/variables";

export async function getProjectVariables(
  projectId: string,
): Promise<SharedVariable[]> {
  const response = await http.get<unknown>(`/projects/${projectId}/variables`);
  return parseSharedVariables(response.data).variables;
}

export async function replaceProjectVariables(
  projectId: string,
  variables: SharedVariableWrite[],
): Promise<SharedVariable[]> {
  const response = await http.put<unknown>(`/projects/${projectId}/variables`, {
    variables,
  });
  return parseSharedVariables(response.data).variables;
}

export async function getEnvironmentVariables(
  environmentId: string,
): Promise<SharedVariable[]> {
  const response = await http.get<unknown>(
    `/environments/${environmentId}/variables`,
  );
  return parseSharedVariables(response.data).variables;
}

export async function replaceEnvironmentVariables(
  environmentId: string,
  variables: SharedVariableWrite[],
): Promise<SharedVariable[]> {
  const response = await http.put<unknown>(
    `/environments/${environmentId}/variables`,
    { variables },
  );
  return parseSharedVariables(response.data).variables;
}
