import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import type { APIRequestContext } from "@playwright/test";

/** Directory holding this file (used to anchor generated artifacts). */
const e2eDir = fileURLToPath(new URL(".", import.meta.url));

/** Base URL of the control plane under test. */
export const baseURL = process.env.GOTHAM_E2E_BASE_URL ?? "http://127.0.0.1:8099";

/** localStorage key the SPA persists its session under. */
export const storageKey = "gotham.auth.session";

/** Playwright storage state written by global setup (authenticated context). */
export const storageStatePath = join(e2eDir, ".auth", "state.json");

/** Credentials + token for the account created in global setup. */
export const accountPath = join(e2eDir, ".auth", "account.json");

/**
 * Public clone URL used by the seeded application. It is only stored as a
 * string — the smoke never triggers a deployment, so nothing is cloned.
 */
export const cloneURL = "https://github.com/docker/welcome-to-docker.git";

/**
 * Address every seeded server row registers.
 *
 * Never 127.0.0.1: the shared self-hosted runner is an all-in-one Gotham host,
 * so its long-lived agent already listens on 127.0.0.1:9443 (docs/test-server.md).
 * Any write that re-routes a node — an application domain, a certificate, a DNS
 * provider or a redirect — makes the control plane dial the seeded address, so
 * 127.0.0.1 reaches that *other* live gRPC server and fails there with
 * `Unimplemented agent.v1.ProxyService` instead of failing fast; the resync
 * then blocks the mutation response and pushes the spec past its timeout.
 *
 * The seeded target must dial nothing and be refused at once on every platform.
 * Plain 127.0.0.2 does not qualify: Linux refuses it, but macOS has no route to
 * non-.1 loopback addresses and the connect hangs until the RPC deadline (which
 * is the same slowdown this constant exists to remove). An explicit unused port
 * on the IPv6 loopback is refused immediately by both, and port 1 is never an
 * agent port, so no live service can answer. The control plane honors a stored
 * `host:port` target verbatim (internal/servers/docker_client.go: agentTargetFor).
 */
export const seedNodeAddress = "[::1]:1";

/** Account created once per run and shared by every scenario. */
export interface E2EAccount {
  email: string;
  password: string;
  accessToken: string;
  userId: string;
}

/** uniqueSuffix namespaces every run's email and application name. */
export function uniqueSuffix(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

/** accountDir is the directory the generated artifacts live in. */
export function accountDir(): string {
  return dirname(storageStatePath);
}

/** loadAccount reads the account written by global setup. */
export function loadAccount(): E2EAccount {
  return JSON.parse(readFileSync(accountPath, "utf8")) as E2EAccount;
}

/**
 * seedProjectEnvironment creates a project (with its `production`
 * environment) through the API. Resource seeds pass the returned
 * environment id since PE-2 requires it.
 */
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

/** environmentURL is the PE-5 environment page for one project/environment. */
export function environmentURL(projectId: string, environmentId: string): string {
  return `/projects/${projectId}/environments/${environmentId}`;
}

/**
 * nestedURL is one resource's detail page under the PE-5 nested routes
 * (/projects/:p/environments/:e/{applications,services,databases}/:id).
 */
export function nestedURL(
  projectId: string,
  environmentId: string,
  kind: "applications" | "services" | "databases",
  id: string,
): string {
  return `/projects/${projectId}/environments/${environmentId}/${kind}/${id}`;
}
