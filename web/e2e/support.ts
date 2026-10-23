import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import type { APIRequestContext } from "@playwright/test";

const e2eDir = fileURLToPath(new URL(".", import.meta.url));

export const baseURL = process.env.GOTHAM_E2E_BASE_URL ?? "http://127.0.0.1:8099";

export const storageKey = "gotham.auth.session";

export const storageStatePath = join(e2eDir, ".auth", "state.json");

export const accountPath = join(e2eDir, ".auth", "account.json");

export const cloneURL = "https://github.com/docker/welcome-to-docker.git";

export const seedNodeAddress = "[::1]:1";

export interface E2EAccount {
  email: string;
  password: string;
  accessToken: string;
  userId: string;
}

export function uniqueSuffix(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export function accountDir(): string {
  return dirname(storageStatePath);
}

export function loadAccount(): E2EAccount {
  return JSON.parse(readFileSync(accountPath, "utf8")) as E2EAccount;
}

export async function seedProjectEnvironment(
  request: APIRequestContext,
  headers: Record<string, string>,
): Promise<{ projectId: string; projectName: string; environmentId: string }> {
  const name = `ui-e2e-${uniqueSuffix()}`;
  const project = await request.post("/api/v1/projects", {
    headers,
    data: { name },
  });
  if (project.status() !== 201) {
    throw new Error(`seed project: ${project.status()} ${await project.text()}`);
  }
  const { project: created, environments } = (await project.json()) as {
    project: { id: string };
    environments: Array<{ id: string }>;
  };
  return { projectId: created.id, projectName: name, environmentId: environments[0].id };
}

export function environmentURL(projectId: string, environmentId: string): string {
  return `/projects/${projectId}/environments/${environmentId}`;
}

export function nestedURL(
  projectId: string,
  environmentId: string,
  kind: "applications" | "services" | "databases",
  id: string,
): string {
  return `/projects/${projectId}/environments/${environmentId}/${kind}/${id}`;
}
